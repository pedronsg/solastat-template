package pluginapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// modbusCallTimeout bounds how long a plugin's ReadRegisters/WriteRegister
// call waits for the core's response, so a core that's stuck or has no
// Modbus connection right now fails the call instead of hanging forever.
const modbusCallTimeout = 5 * time.Second

// ModbusAccess is the register read/write contract a plugin needing Modbus
// access (e.g. gridcharge) is given — requests are proxied to the core's
// single *modbus.Client over the same stdio connection everything else
// uses. A plugin that doesn't need it just never calls it.
type ModbusAccess interface {
	ReadRegisters(slaveID, fc byte, address, count uint16) ([]uint16, error)
	WriteRegister(slaveID byte, address, value uint16) error
}

// Plugin is what a plugin's constructor returns: Hooks plus the ability to
// report its own status for the ready/authorized handshake.
type Plugin interface {
	Hooks
	Info() Info
}

// ServeConfig configures Serve.
type ServeConfig struct {
	// New constructs the plugin. deviceSerial and getKeys let it verify its
	// own license key (see solastat-auth) — getKeys always returns the
	// current pool, updated live as the core relays newly activated keys,
	// no restart needed. modbus is always non-nil; plugins that don't need
	// register access simply never call it.
	New func(deviceSerial string, getKeys func() []string, modbus ModbusAccess) (Plugin, http.Handler)

	// Route is where the core mounts Handler (e.g. "/relay"), if New
	// returns a non-nil http.Handler. Ignored otherwise.
	Route string

	// In/Out override the protocol streams — default to os.Stdin/os.Stdout.
	// Only ever set in tests; a real plugin leaves these nil.
	In  io.Reader
	Out io.Writer
}

// Serve implements a plugin's entire side of the core↔plugin protocol —
// run it as the whole of main(). Blocks until the core sends "shutdown" or
// its stdin closes (e.g. the core process died), at which point it returns
// nil so main() can exit.
func Serve(cfg ServeConfig) error {
	in, out := cfg.In, cfg.Out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	stdin := bufio.NewReader(in)

	hello, err := ReadEnvelope(stdin)
	if err != nil {
		return fmt.Errorf("pluginapi: read hello: %w", err)
	}
	if hello.Type != TypeHello {
		return fmt.Errorf("pluginapi: expected %q as the first message, got %q", TypeHello, hello.Type)
	}

	var writeMu sync.Mutex
	write := func(e Envelope) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return WriteEnvelope(out, e)
	}

	var keysMu sync.RWMutex
	keys := hello.Keys
	getKeys := func() []string {
		keysMu.RLock()
		defer keysMu.RUnlock()
		return append([]string{}, keys...)
	}

	mb := newModbusRequester(write)

	plugin, handler := cfg.New(hello.DeviceSerial, getKeys, mb)

	httpPort := 0
	if handler != nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("pluginapi: listen for HTTP: %w", err)
		}
		httpPort = ln.Addr().(*net.TCPAddr).Port
		go http.Serve(ln, handler) //nolint:errcheck // server lifetime == process lifetime
	}

	info := plugin.Info()
	if err := write(Envelope{Type: TypeReady, ID: info.ID, Version: info.Version, HTTPPort: httpPort, Route: cfg.Route}); err != nil {
		return fmt.Errorf("pluginapi: write ready: %w", err)
	}

	var lastAuthorized atomic.Bool
	lastAuthorized.Store(info.Authorized)
	reportAuthorization := func() {
		now := plugin.Info().Authorized
		if now != lastAuthorized.Swap(now) {
			_ = write(Envelope{Type: TypeAuthorized, Value: now})
		}
	}

	// work carries reading/tick/keys envelopes to a dedicated goroutine
	// that processes them one at a time, in order. This must NOT be the
	// same goroutine that reads stdin below: OnReading/Tick are allowed to
	// make blocking ModbusAccess calls, which wait for a
	// modbus_read_resp/modbus_write_resp that only the stdin-reading loop
	// can deliver — if that loop were the one blocked inside Tick(), it
	// could never read the very response unblocking it.
	work := make(chan Envelope, 16)
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		for e := range work {
			switch e.Type {
			case TypeKeys:
				keysMu.Lock()
				keys = e.Keys
				keysMu.Unlock()
			case TypeReading:
				plugin.OnReading(e.Readings)
				reportAuthorization()
			case TypeTick:
				plugin.Tick()
				reportAuthorization()
			}
		}
	}()

	for {
		e, err := ReadEnvelope(stdin)
		if err != nil {
			close(work)
			<-workDone
			return nil // stdin closed — core exited (or sent malformed input); either way, stop.
		}
		switch e.Type {
		case TypeShutdown:
			close(work)
			<-workDone
			return nil
		case TypeModbusReadResp, TypeModbusWriteResp:
			mb.deliver(e) // handled inline: never queued behind a possibly-blocked OnReading/Tick
		default:
			work <- e
		}
	}
}

// modbusRequester implements ModbusAccess by sending modbus_read/write
// over the protocol connection and blocking for the matching *_resp.
type modbusRequester struct {
	write  func(Envelope) error
	nextID atomic.Uint64

	pendingMu sync.Mutex
	pending   map[string]chan Envelope
}

func newModbusRequester(write func(Envelope) error) *modbusRequester {
	return &modbusRequester{write: write, pending: make(map[string]chan Envelope)}
}

func (m *modbusRequester) deliver(e Envelope) {
	m.pendingMu.Lock()
	ch, ok := m.pending[e.RequestID]
	if ok {
		delete(m.pending, e.RequestID)
	}
	m.pendingMu.Unlock()
	if ok {
		ch <- e
	}
}

func (m *modbusRequester) call(req Envelope) (Envelope, error) {
	id := strconv.FormatUint(m.nextID.Add(1), 10)
	req.RequestID = id
	ch := make(chan Envelope, 1)
	m.pendingMu.Lock()
	m.pending[id] = ch
	m.pendingMu.Unlock()

	if err := m.write(req); err != nil {
		m.pendingMu.Lock()
		delete(m.pending, id)
		m.pendingMu.Unlock()
		return Envelope{}, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(modbusCallTimeout):
		m.pendingMu.Lock()
		delete(m.pending, id)
		m.pendingMu.Unlock()
		return Envelope{}, fmt.Errorf("pluginapi: modbus request timed out")
	}
}

func (m *modbusRequester) ReadRegisters(slaveID, fc byte, address, count uint16) ([]uint16, error) {
	resp, err := m.call(Envelope{Type: TypeModbusRead, SlaveID: slaveID, FC: fc, Address: address, Count: count})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Regs, nil
}

func (m *modbusRequester) WriteRegister(slaveID byte, address, value uint16) error {
	resp, err := m.call(Envelope{Type: TypeModbusWrite, SlaveID: slaveID, Address: address, RegValue: value})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
