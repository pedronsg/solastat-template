// Package pluginclient is the reusable client every solastat plugin
// (relay, gridcharge, ...) imports to talk to the core app's PluginHub gRPC
// service: exchange the raw license-key pool, subscribe to solar snapshots,
// and proxy Modbus register reads/writes.
//
// The core never decides authorization — it only relays whatever keys the
// user pasted into Settings. Client accepts a checkAuthorized callback
// (typically backed by solastat-auth.Authorizes) that the caller supplies
// to verify those keys itself, and reports the verdict back to the core on
// the next Register call, purely for Settings display.
//
// It mirrors the connect/reconnect shape already used elsewhere in this
// codebase (e.g. the OrbitOS device client, the old fsipc.Client): construct
// once, it connects in the background and keeps retrying — callers don't
// need to coordinate startup order with the core app.
package pluginclient

import (
	"context"
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
	// reregisterInterval re-sends Register (and re-runs checkAuthorized
	// against the refreshed key pool) on a fixed cadence, so pasting a new
	// key into the core's Settings page unlocks a running plugin without
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
	target          string
	pluginID        string
	pluginVersion   string
	checkAuthorized func(keys []string) bool

	mu         sync.RWMutex
	conn       *grpc.ClientConn
	hub        pb.PluginHubClient
	authorized bool

	authMu   sync.Mutex
	authSubs []func(authorized bool)

	dataMu   sync.Mutex
	dataSubs []func(*pb.SolarSnapshot)

	closeOnce sync.Once
	done      chan struct{}
}

// New starts connecting in the background and returns immediately. Call
// Close on shutdown to stop the background reconnect loop.
//
// checkAuthorized is called with the raw key pool the core is currently
// holding, on every (re)register — implement it with
// solastat-auth.Authorizes(key, myDeviceHash, pluginID) for each key.
func New(socketPath, pluginID, pluginVersion string, checkAuthorized func(keys []string) bool) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	c := &Client{
		target:          "unix:" + socketPath,
		pluginID:        pluginID,
		pluginVersion:   pluginVersion,
		checkAuthorized: checkAuthorized,
		done:            make(chan struct{}),
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

// OnAuthorization registers fn to be called whenever the locally-verified
// authorization state changes (including the first check). Plugins should
// start/stop their automation loop from this callback instead of assuming
// authorization never changes at runtime.
func (c *Client) OnAuthorization(fn func(authorized bool)) {
	c.authMu.Lock()
	c.authSubs = append(c.authSubs, fn)
	c.mu.RLock()
	authorized := c.authorized
	c.mu.RUnlock()
	c.authMu.Unlock()
	fn(authorized)
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
// re-registering to re-check the key pool for a freshly activated license.
// Returns when the stream ends (connection dropped) so connectLoop can
// redial.
func (c *Client) runSession(conn *grpc.ClientConn) {
	c.register()

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
			c.register()
		}
	}
}

// register reports the last-known authorized verdict to the core (purely
// for its Settings display), fetches the current raw key pool, re-checks it
// with checkAuthorized, and fires OnAuthorization subscribers if the
// verdict changed. The core's response is never trusted for the
// authorization decision itself — only checkAuthorized's return value is.
func (c *Client) register() bool {
	c.mu.RLock()
	hub := c.hub
	lastAuthorized := c.authorized
	c.mu.RUnlock()
	if hub == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	resp, err := hub.Register(ctx, &pb.RegisterRequest{
		PluginId:      c.pluginID,
		PluginVersion: c.pluginVersion,
		Authorized:    lastAuthorized,
	})
	if err != nil {
		return lastAuthorized
	}

	authorized := false
	if c.checkAuthorized != nil {
		authorized = c.checkAuthorized(resp.Keys)
	}

	c.mu.Lock()
	changed := c.authorized != authorized
	c.authorized = authorized
	c.mu.Unlock()

	if changed {
		c.authMu.Lock()
		subs := append([]func(bool){}, c.authSubs...)
		c.authMu.Unlock()
		for _, fn := range subs {
			fn(authorized)
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
