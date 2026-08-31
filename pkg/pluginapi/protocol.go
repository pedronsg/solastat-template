package pluginapi

import (
	"bufio"
	"encoding/json"
	"io"
)

// WriteEnvelope writes e as one JSON line. Exported so both sides of the
// protocol (the core's plugin host, and Serve here) share one
// implementation of the wire format instead of two that could drift.
func WriteEnvelope(w io.Writer, e Envelope) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ReadEnvelope reads and decodes one JSON line.
func ReadEnvelope(r *bufio.Reader) (Envelope, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Envelope{}, err
	}
	var e Envelope
	if jerr := json.Unmarshal(line, &e); jerr != nil {
		return Envelope{}, jerr
	}
	return e, nil
}

// Wire protocol between the core (solastat-omie) and a plugin subprocess:
// newline-delimited JSON, core → plugin on the plugin's stdin, plugin →
// core on the plugin's stdout. One envelope per line, Type selects which
// other fields are meaningful — everything else is zero. Chosen instead of
// gRPC or Go's native plugin package deliberately: no cgo, no version
// lock-step between core and plugin builds, no separate listener/port for
// the control channel — just a subprocess and two pipes, which is also all
// a plugin binary needs to be discoverable ("copy it into the plugins
// directory") without ever being its own installed OrbitOS app.

// Envelope is the single message type sent on either stream.
type Envelope struct {
	Type string `json:"type"`

	// core → plugin
	DeviceSerial string             `json:"device_serial,omitempty"` // hello only
	Keys         []string           `json:"keys,omitempty"`          // hello, keys
	TsUnixMs     int64              `json:"ts_unix_ms,omitempty"`    // reading
	Error        string             `json:"error,omitempty"`         // reading, modbus_*_resp
	Readings     map[string]Reading `json:"readings,omitempty"`      // reading

	// plugin → core
	ID       string `json:"id,omitempty"`        // ready
	Version  string `json:"version,omitempty"`   // ready
	HTTPPort int    `json:"http_port,omitempty"` // ready — 0 if the plugin serves no HTTP page
	Route    string `json:"route,omitempty"`     // ready — e.g. "/relay"; ignored if HTTPPort is 0
	Value    bool   `json:"value,omitempty"`     // authorized

	// modbus_read / modbus_read_resp / modbus_write / modbus_write_resp,
	// both directions — RequestID correlates a response to its request
	// (the single stdio stream can have more than one in flight). SlaveID
	// is also sent standalone (hello, slave_id) to tell a plugin needing
	// Modbus access which slave address the core's active inverter profile
	// currently uses — it can change if the user switches profiles, so
	// ModbusAccess always reflects the latest one sent, not just hello's.
	RequestID string   `json:"request_id,omitempty"`
	SlaveID   byte     `json:"slave_id,omitempty"`
	FC        byte     `json:"fc,omitempty"`
	Address   uint16   `json:"address,omitempty"`
	Count     uint16   `json:"count,omitempty"`
	RegValue  uint16   `json:"reg_value,omitempty"`
	Regs      []uint16 `json:"regs,omitempty"`

	// log (plugin → core, optional — forwarded into the core's own logger
	// under this plugin's ID as tag)
	Level string `json:"level,omitempty"`
	Text  string `json:"text,omitempty"`
}

const (
	// core → plugin
	TypeHello    = "hello"    // first message: DeviceSerial, Keys, SlaveID
	TypeKeys     = "keys"     // Keys pool changed — Keys
	TypeSlaveID  = "slave_id" // active Modbus slave address changed — SlaveID
	TypeReading  = "reading"  // one poll cycle — TsUnixMs, Error, Readings
	TypeTick     = "tick"     // fixed-interval tick, no payload
	TypeShutdown = "shutdown" // core is exiting, no payload

	// plugin → core
	TypeReady      = "ready"      // first message: ID, Version, HTTPPort, Route
	TypeAuthorized = "authorized" // authorization verdict changed — Value
	TypeLog        = "log"        // Level, Text

	// both directions
	TypeModbusRead      = "modbus_read"       // plugin → core: RequestID, SlaveID, FC, Address, Count
	TypeModbusReadResp  = "modbus_read_resp"  // core → plugin: RequestID, Regs, Error
	TypeModbusWrite     = "modbus_write"      // plugin → core: RequestID, SlaveID, Address, RegValue
	TypeModbusWriteResp = "modbus_write_resp" // core → plugin: RequestID, Error
)
