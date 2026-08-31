// Package pluginapi is the shared contract between solastat-omie (the
// core) and every plugin package (relay, gridcharge, ...): a plugin is
// compiled directly into the core binary via a Go build tag, and just needs
// to expose something satisfying Hooks. There is no process boundary, no
// RPC — the core calls these methods directly.
//
// This package has zero external dependencies (not even the OrbitOS SDK) so
// it never drags anything into a build that imports it.
package pluginapi

// Reading mirrors one decoded register value from a poll cycle. The core
// builds a map[string]Reading from its own internal/solar.Data and passes
// it to every compiled-in plugin's OnReading.
type Reading struct {
	Label string
	Value float64
	Unit  string
}

// Hooks is what a compiled-in plugin exposes to the core's build-tag-gated
// wiring code. Both methods must be safe to call even while the plugin is
// unauthorized — a plugin gates its own behavior internally (see
// solastat-auth) rather than relying on the core to withhold calls.
type Hooks interface {
	// OnReading is called once per completed solar poll cycle.
	OnReading(readings map[string]Reading)
	// Tick is called on a fixed short interval (typically 1s), for
	// time-based state (timeouts, cooldowns) independent of the poll cycle.
	Tick()
}

// Info describes one compiled-in plugin, for the Settings page's plugin
// list — populated by the plugin's wiring code, never guessed by the core.
type Info struct {
	ID         string
	Version    string
	Authorized bool
}
