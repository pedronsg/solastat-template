// Package pluginapi is the shared contract between solastat-omie (the
// core) and every plugin (relay, gridcharge, ...). A plugin is a plain Go
// binary the core discovers in a directory and launches as a subprocess —
// never compiled into the core, never installed as its own OrbitOS app.
// They talk over the plugin's stdin/stdout using the newline-delimited
// JSON protocol in protocol.go; Serve (in serve.go) implements the
// plugin's entire side of it, so a plugin author only has to provide a
// Hooks implementation and, optionally, an http.Handler.
//
// This package has no dependency beyond the Go standard library, so it
// never drags anything into a build that imports it.
package pluginapi

// Reading mirrors one decoded register value from a poll cycle. The core
// builds a map[string]Reading from its own internal/solar.Data and sends
// it to every running plugin's OnReading.
type Reading struct {
	Label string  `json:"label,omitempty"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// Hooks is what a plugin's Serve call is given. Both methods must be safe
// to call even while the plugin is unauthorized — a plugin gates its own
// behavior internally (see solastat-auth) rather than relying on the core
// to withhold calls.
type Hooks interface {
	// OnReading is called once per completed solar poll cycle.
	OnReading(readings map[string]Reading)
	// Tick is called on a fixed short interval (~1s), for time-based state
	// (timeouts, cooldowns) independent of the poll cycle. A plugin that
	// only needs a slower cadence (e.g. gridcharge's 30s decision cycle)
	// rate-limits itself internally rather than asking the core for a
	// different interval — the core's tick cadence is the same for every
	// plugin, by design, so it never needs to know anything
	// plugin-specific.
	Tick()
}

// Info describes a running plugin, for the Settings page's plugin list —
// reported by the plugin itself, never guessed by the core.
type Info struct {
	ID         string `json:"id"`
	Version    string `json:"version,omitempty"`
	Authorized bool   `json:"authorized"`
}
