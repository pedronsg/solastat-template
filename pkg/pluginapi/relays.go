package pluginapi

import "time"

// RelayState is one relay a plugin drives, as a screen on the device shows
// it (the SolaStat Pro's LCD): whether it is on, in which mode and why.
type RelayState struct {
	ID      int       `json:"id"`              // the board's relay number, from 1
	On      bool      `json:"on"`              // the contact is closed
	Since   time.Time `json:"since,omitempty"` // when it last switched; zero before the first
	Enabled bool      `json:"enabled"`         // the automation is on
	Mode    string    `json:"mode"`            // the plugin's mode ID (surplus, soc, ...)
	// Reason is why the relay is as it is; Waiting a change its delays or
	// minimum times are holding back, nil when there is none.
	Reason  RelayReason  `json:"reason"`
	Waiting *RelayReason `json:"waiting,omitempty"`
	// OnToday is how long it has been on since midnight.
	OnToday time.Duration `json:"on_today"`
}

// RelayReason is a reason as a translation key, the values its text takes,
// and that text in English for a screen without the key.
type RelayReason struct {
	Key  string            `json:"key"`
	Args map[string]string `json:"args,omitempty"`
	Text string            `json:"text"`
}

// RelayStateProvider is an optional interface a compiled-in plugin that
// drives the board's relays implements, for a screen on the device. The core
// checks for it via a type assertion; RelayStates is nil while the plugin is
// unauthorized, and it is cheap enough to be called every second.
type RelayStateProvider interface {
	RelayStates() []RelayState
}
