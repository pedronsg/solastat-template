package pluginapi

// Fault is one fault or alarm of the inverter, as the core reads it from
// the active inverter profile (its fault registers, inverter_status codes
// or fault_state).
type Fault struct {
	// ID is the same for a given fault of a given profile on every poll,
	// so a plugin can store it in its settings: "<register>.<bit>" for a
	// fault register bit, "<register>=<code>" for a code,
	// "inverter_status=<code>" or "<reading>=<value>" (fault_state).
	ID       string `json:"id"`
	Code     string `json:"code,omitempty"` // what the vendor shows ("F18", "2032"); may be empty
	Name     string `json:"name,omitempty"` // may be empty
	Group    string `json:"group,omitempty"`
	Severity string `json:"severity"` // FaultSeverityFault or FaultSeverityAlarm
}

// The Fault severities.
const (
	FaultSeverityFault = "fault"
	FaultSeverityAlarm = "alarm"
)

// FaultReport is the inverter's fault state in one poll cycle. Known is
// false when the cycle couldn't tell (a communication error, or a profile
// that reports no faults at all); Active is then empty.
type FaultReport struct {
	Known  bool    `json:"known"`
	Active []Fault `json:"active"`
}

// FaultObserver is an optional interface a compiled-in plugin implements to
// get the inverter's faults after every poll cycle, right after OnReading.
// Like OnReading, it must be safe to call while the plugin is unauthorized.
type FaultObserver interface {
	OnFaults(FaultReport)
}

// The FaultCatalog kinds: what the active inverter profile can tell.
const (
	// FaultsNamed: fault registers, each fault by ID, code and/or name.
	FaultsNamed = "registers"
	// FaultsCodes: an inverter_status error code (0 = OK); codes outside
	// the catalog can still occur.
	FaultsCodes = "status"
	// FaultsState: only "in fault or not", from the profile's fault_state.
	FaultsState = "state"
	// FaultsNone: the profile reports no faults.
	FaultsNone = "none"
)

// FaultCatalog is every fault the active inverter profile can report, so a
// plugin's settings page can offer them by code and name.
type FaultCatalog struct {
	Kind   string  `json:"kind"`   // FaultsNamed, FaultsCodes, FaultsState or FaultsNone
	Faults []Fault `json:"faults"` // empty for FaultsNone
}
