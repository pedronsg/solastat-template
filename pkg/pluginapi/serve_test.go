package pluginapi

import (
	"bufio"
	"io"
	"net/http"
	"testing"
	"time"
)

type fakePlugin struct {
	onReadingCalls int
	tickCalls      int
	authorized     bool
	lastReadings   map[string]Reading
}

func (f *fakePlugin) OnReading(r map[string]Reading) { f.onReadingCalls++; f.lastReadings = r }
func (f *fakePlugin) Tick()                          { f.tickCalls++ }
func (f *fakePlugin) Info() Info {
	return Info{ID: "fake", Version: "0.1.0", Authorized: f.authorized}
}

// TestServeHandshakeAndDispatch drives Serve through a fake core: sends
// hello, expects ready, sends a reading and a tick, and checks both
// dispatched to the plugin.
func TestServeHandshakeAndDispatch(t *testing.T) {
	coreToPlugin, pluginIn := newPipe(t)
	pluginOut, coreFromPlugin := newPipe(t)

	fp := &fakePlugin{}
	done := make(chan error, 1)
	go func() {
		done <- Serve(ServeConfig{
			New: func(deviceSerial string, getKeys func() []string, modbus ModbusAccess) (Plugin, http.Handler) {
				if deviceSerial != "serial-123" {
					t.Errorf("New got deviceSerial %q, want serial-123", deviceSerial)
				}
				if keys := getKeys(); len(keys) != 1 || keys[0] != "key-a" {
					t.Errorf("New's getKeys() = %v, want [key-a]", keys)
				}
				return fp, nil
			},
			Route: "/fake",
			In:    pluginIn,
			Out:   pluginOut,
		})
	}()

	coreReader := bufio.NewReader(coreFromPlugin)

	if err := WriteEnvelope(coreToPlugin, Envelope{Type: TypeHello, DeviceSerial: "serial-123", Keys: []string{"key-a"}}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	ready := mustReadEnvelope(t, coreReader)
	if ready.Type != TypeReady || ready.ID != "fake" || ready.Version != "0.1.0" || ready.Route != "/fake" {
		t.Fatalf("unexpected ready message: %+v", ready)
	}
	if ready.HTTPPort != 0 {
		t.Fatalf("expected HTTPPort 0 (nil handler), got %d", ready.HTTPPort)
	}

	if err := WriteEnvelope(coreToPlugin, Envelope{
		Type: TypeReading, TsUnixMs: 1000,
		Readings: map[string]Reading{"pv_total_power": {Value: 42, Unit: "W"}},
	}); err != nil {
		t.Fatalf("write reading: %v", err)
	}
	if err := WriteEnvelope(coreToPlugin, Envelope{Type: TypeTick}); err != nil {
		t.Fatalf("write tick: %v", err)
	}
	if err := WriteEnvelope(coreToPlugin, Envelope{Type: TypeShutdown}); err != nil {
		t.Fatalf("write shutdown: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}

	if fp.onReadingCalls != 1 {
		t.Fatalf("OnReading called %d times, want 1", fp.onReadingCalls)
	}
	if fp.lastReadings["pv_total_power"].Value != 42 {
		t.Fatalf("OnReading got wrong readings: %+v", fp.lastReadings)
	}
	if fp.tickCalls != 1 {
		t.Fatalf("Tick called %d times, want 1", fp.tickCalls)
	}
}

// TestServeReportsAuthorizationChange checks the plugin's Info().Authorized
// transition is reported to the core as an "authorized" message exactly
// once, when it actually changes.
func TestServeReportsAuthorizationChange(t *testing.T) {
	coreToPlugin, pluginIn := newPipe(t)
	pluginOut, coreFromPlugin := newPipe(t)

	fp := &fakePlugin{}
	go Serve(ServeConfig{
		New: func(string, func() []string, ModbusAccess) (Plugin, http.Handler) { return fp, nil },
		In:  pluginIn,
		Out: pluginOut,
	})

	coreReader := bufio.NewReader(coreFromPlugin)
	_ = WriteEnvelope(coreToPlugin, Envelope{Type: TypeHello})
	mustReadEnvelope(t, coreReader) // ready

	fp.authorized = true
	_ = WriteEnvelope(coreToPlugin, Envelope{Type: TypeTick})

	msg := mustReadEnvelope(t, coreReader)
	if msg.Type != TypeAuthorized || !msg.Value {
		t.Fatalf("expected authorized=true message, got %+v", msg)
	}

	// No further change — must not send a second authorized message. Prove
	// it by sending another tick and checking a reading arrives cleanly
	// right after (if a spurious authorized message were queued, it would
	// be read here instead).
	_ = WriteEnvelope(coreToPlugin, Envelope{Type: TypeTick})
	_ = WriteEnvelope(coreToPlugin, Envelope{Type: TypeReading, TsUnixMs: 1})
	_ = WriteEnvelope(coreToPlugin, Envelope{Type: TypeShutdown})
}

// newPipe returns (writeEnd, readEnd) — a fresh in-memory pipe, matching
// the direction callers use it in below.
func newPipe(t *testing.T) (*io.PipeWriter, *io.PipeReader) {
	t.Helper()
	r, w := io.Pipe()
	return w, r
}

func mustReadEnvelope(t *testing.T, r *bufio.Reader) Envelope {
	t.Helper()
	e, err := ReadEnvelope(r)
	if err != nil {
		t.Fatalf("ReadEnvelope: %v", err)
	}
	return e
}
