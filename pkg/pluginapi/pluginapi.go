// Package pluginapi is the shared contract between solastat-omie (the
// public core) and its plugins (relay, gridcharge, ...). Plugins are
// compiled directly into the core's binary, gated behind a Go build tag
// (see solastat-omie's cmd/solastat-omie/plugins.go) — their actual
// implementation lives in a private repo the core only depends on with
// that tag active. These three types are the one thing both sides need to
// agree on regardless: the core's untagged (public, dependency-free)
// build still declares an interface naming them, so this package has to
// stay importable without pulling in anything private.
//
// This package has no dependency beyond the Go standard library, so it
// never drags anything into a build that imports it.
package pluginapi

// Reading mirrors one decoded register value from a poll cycle. The core
// builds a map[string]Reading from its own internal/solar.Data and calls
// every compiled-in plugin's OnReading with it directly.
type Reading struct {
	Label string  `json:"label,omitempty"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// Hooks is what the core calls directly on a compiled-in plugin. Both
// methods must be safe to call even while the plugin is unauthorized — a
// plugin gates its own behavior internally (see solastat-auth) rather than
// relying on the core to withhold calls.
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

// LogEvent is one entry a plugin reports for the core's dashboard activity
// log — e.g. relay's Kind "relay" (On true/false) or its own read of the
// inverter's status register (Kind "error"/"ok", Code the raw value), or
// gridcharge's free-text write-attempt log (Text, OK). The core timestamps
// and tags it with the reporting plugin's ID before storing it — a plugin
// only ever describes what happened, never where or when it's kept.
type LogEvent struct {
	Kind string `json:"kind,omitempty"`
	On   bool   `json:"on,omitempty"`
	Code int    `json:"code,omitempty"`
	Text string `json:"text,omitempty"`
	OK   bool   `json:"ok,omitempty"`
}
