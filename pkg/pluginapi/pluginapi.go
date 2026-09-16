// Package pluginapi is the shared contract between solastat (the
// public core) and its plugins (relay, gridcharge, ...). Plugins are
// compiled directly into the core's binary, gated behind a Go build tag
// (see solastat's cmd/solastat/plugins.go) — their actual
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
//
// NotifyType and Text double as the mobile-notification path: when
// NotifyType is non-empty and Text is non-empty, the core looks up
// "<plugin ID>:<NotifyType>" in the user's notification settings and, if
// enabled there, pushes Text as the notification body via
// MobileNotificationManager. Leave NotifyType empty for events that should
// only ever appear in the activity log (e.g. relay's routine "ok" status
// sighting) — the two are independent, a plugin decides per event which
// applies. NotifyType must match one of the IDs the plugin returns from
// NotificationTypes (see below); the core never invents its own.
type LogEvent struct {
	Kind       string `json:"kind,omitempty"`
	On         bool   `json:"on,omitempty"`
	Code       int    `json:"code,omitempty"`
	Text       string `json:"text,omitempty"`
	OK         bool   `json:"ok,omitempty"`
	NotifyType string `json:"notify_type,omitempty"`
}

// NotificationType is one kind of mobile notification a plugin can fire,
// as shown on the Settings → Notifications page. The core never hardcodes
// plugin-specific notification types — any compiled-in plugin that
// implements NotificationTypeProvider gets its own checkbox automatically,
// namespaced by the plugin's own ID so two plugins can never collide.
type NotificationType struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	DefaultEnabled bool   `json:"default_enabled"`
}

// NotificationTypeProvider is an optional interface a compiled-in plugin
// implements to declare which of its LogEvents are notification-worthy.
// The core checks for it via a type assertion (like it already does for an
// optional Close() error) — a plugin that doesn't implement it just never
// gets a notification checkbox, it isn't required to.
type NotificationTypeProvider interface {
	NotificationTypes() []NotificationType
}
