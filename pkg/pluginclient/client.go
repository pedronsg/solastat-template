// Package pluginclient is the reusable client every solastat plugin
// (relay, gridcharge, auth, ...) imports to talk to the core app's
// PluginHub gRPC service: register + license handshake, subscribe to solar
// snapshots, and proxy Modbus register reads/writes.
//
// It mirrors the connect/reconnect shape already used elsewhere in this
// codebase (e.g. the OrbitOS device client, the old fsipc.Client): construct
// once, it connects in the background and keeps retrying — callers don't
// need to coordinate startup order with the core app.
package pluginclient

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/pedronsg/solastat-template/proto/gen"
)

const (
	dialTimeout    = 3 * time.Second
	rpcTimeout     = 3 * time.Second
	reconnectDelay = 2 * time.Second
	// reregisterInterval re-sends Register while unauthorized, so pasting a
	// license key into the Settings page unlocks a running plugin without
	// requiring a restart.
	reregisterInterval = 15 * time.Second
)

// DefaultSocketPath is the well-known Unix socket the core app's PluginHub
// listens on. Override per-deployment via the -pluginhub-socket flag.
const DefaultSocketPath = "/tmp/orbitos/solastat-omie-pluginhub.sock"

// Client connects to a core app's PluginHub over a local Unix domain
// socket. Implements the same ReadRegisters/WriteRegister method shape used
// by gridcharge.ModbusAccess, so it's a drop-in Modbus transport for
// automation packages ported from the monolith.
type Client struct {
	target         string
	pluginID       string
	pluginVersion  string
	licenseKeyPath string

	mu         sync.RWMutex
	conn       *grpc.ClientConn
	hub        pb.PluginHubClient
	authorized bool
	reason     string

	authMu   sync.Mutex
	authSubs []func(authorized bool, reason string)

	dataMu   sync.Mutex
	dataSubs []func(*pb.SolarSnapshot)

	closeOnce sync.Once
	done      chan struct{}
}

// New starts connecting in the background and returns immediately. Call
// Close on shutdown to stop the background reconnect loop.
func New(socketPath, pluginID, pluginVersion, licenseKeyPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	c := &Client{
		target:         "unix:" + socketPath,
		pluginID:       pluginID,
		pluginVersion:  pluginVersion,
		licenseKeyPath: licenseKeyPath,
		done:           make(chan struct{}),
	}
	go c.connectLoop()
	return c
}

// Close stops the background reconnect loop and closes any active
// connection. The Client must not be used afterwards.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	})
}

// OnAuthorization registers fn to be called whenever the authorization
// state changes (including the first Register response). Plugins should
// start/stop their automation loop from this callback instead of assuming
// authorization never changes at runtime.
func (c *Client) OnAuthorization(fn func(authorized bool, reason string)) {
	c.authMu.Lock()
	c.authSubs = append(c.authSubs, fn)
	c.mu.RLock()
	authorized, reason := c.authorized, c.reason
	c.mu.RUnlock()
	c.authMu.Unlock()
	fn(authorized, reason)
}

// OnSnapshot registers fn to be called with every solar poll-cycle
// snapshot broadcast by the core.
func (c *Client) OnSnapshot(fn func(*pb.SolarSnapshot)) {
	c.dataMu.Lock()
	c.dataSubs = append(c.dataSubs, fn)
	c.dataMu.Unlock()
}

// Authorized reports the last known authorization state.
func (c *Client) Authorized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authorized
}

func (c *Client) connectLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		conn, err := grpc.NewClient(c.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			c.wait(reconnectDelay)
			continue
		}

		c.mu.Lock()
		c.conn = conn
		c.hub = pb.NewPluginHubClient(conn)
		c.mu.Unlock()

		c.runSession(conn)

		c.mu.Lock()
		if c.conn == conn {
			conn.Close()
			c.conn = nil
			c.hub = nil
		}
		c.mu.Unlock()

		select {
		case <-c.done:
			return
		default:
		}
		c.wait(reconnectDelay)
	}
}

func (c *Client) wait(d time.Duration) {
	select {
	case <-c.done:
	case <-time.After(d):
	}
}

// runSession registers, then streams solar data while periodically
// re-registering to pick up a freshly activated license. Returns when the
// stream ends (connection dropped) so connectLoop can redial.
func (c *Client) runSession(conn *grpc.ClientConn) {
	if !c.register() {
		// Even if unauthorized, stay connected and keep polling Register —
		// activating the license in Settings should not require a restart.
	}

	reregister := time.NewTicker(reregisterInterval)
	defer reregister.Stop()

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		c.streamSnapshots(conn)
	}()

	for {
		select {
		case <-c.done:
			return
		case <-streamDone:
			return
		case <-reregister.C:
			if !c.Authorized() {
				c.register()
			}
		}
	}
}

func (c *Client) register() bool {
	c.mu.RLock()
	hub := c.hub
	c.mu.RUnlock()
	if hub == nil {
		return false
	}

	key := ""
	if c.licenseKeyPath != "" {
		if b, err := os.ReadFile(c.licenseKeyPath); err == nil {
			key = strings.TrimSpace(string(b))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	resp, err := hub.Register(ctx, &pb.RegisterRequest{
		PluginId:      c.pluginID,
		PluginVersion: c.pluginVersion,
		LicenseKey:    key,
	})

	var authorized bool
	var reason string
	if err != nil {
		authorized, reason = false, "register rpc failed: "+err.Error()
	} else {
		authorized, reason = resp.Authorized, resp.Reason
	}

	c.mu.Lock()
	changed := c.authorized != authorized || c.reason != reason
	c.authorized, c.reason = authorized, reason
	c.mu.Unlock()

	if changed {
		c.authMu.Lock()
		subs := append([]func(bool, string){}, c.authSubs...)
		c.authMu.Unlock()
		for _, fn := range subs {
			fn(authorized, reason)
		}
	}
	return authorized
}

func (c *Client) streamSnapshots(conn *grpc.ClientConn) {
	hub := pb.NewPluginHubClient(conn)
	stream, err := hub.SubscribeSolarData(context.Background(), &pb.SubscribeRequest{})
	if err != nil {
		return
	}
	for {
		snap, err := stream.Recv()
		if err != nil {
			return
		}
		c.dataMu.Lock()
		subs := append([]func(*pb.SolarSnapshot){}, c.dataSubs...)
		c.dataMu.Unlock()
		for _, fn := range subs {
			fn(snap)
		}
	}
}

// ReadRegisters satisfies the same shape as gridcharge.ModbusAccess.
func (c *Client) ReadRegisters(slaveID, fc byte, address, count uint16) ([]uint16, error) {
	c.mu.RLock()
	hub := c.hub
	c.mu.RUnlock()
	if hub == nil {
		return nil, errNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	resp, err := hub.ReadRegisters(ctx, &pb.ReadRegistersRequest{
		SlaveId: uint32(slaveID), Fc: uint32(fc), Address: uint32(address), Count: uint32(count),
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errString(resp.Error)
	}
	regs := make([]uint16, len(resp.Regs))
	for i, v := range resp.Regs {
		regs[i] = uint16(v)
	}
	return regs, nil
}

// WriteRegister satisfies the same shape as gridcharge.ModbusAccess.
func (c *Client) WriteRegister(slaveID byte, address, value uint16) error {
	c.mu.RLock()
	hub := c.hub
	c.mu.RUnlock()
	if hub == nil {
		return errNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	resp, err := hub.WriteRegister(ctx, &pb.WriteRegisterRequest{
		SlaveId: uint32(slaveID), Address: uint32(address), Value: uint32(value),
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errString(resp.Error)
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

const errNotConnected = errString("pluginclient: not connected to core PluginHub")
