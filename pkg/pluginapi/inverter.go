package pluginapi

import "time"

// BatteryAction is one thing InverterControl can force the inverter's
// battery to do, overriding its own energy management while enabled.
type BatteryAction string

const (
	// ForceCharge charges the battery from the grid.
	ForceCharge BatteryAction = "force_charge"
	// ForceExport discharges the battery into the grid.
	ForceExport BatteryAction = "force_export"
)

// BatteryTarget is what an action aims for while it's enabled.
type BatteryTarget struct {
	// SocPercent is where the action stops: a ceiling for ForceCharge, a
	// floor for ForceExport.
	SocPercent int
	// PowerWatts is the charge/export power to request. An inverter with no
	// power setting for an action ignores it.
	PowerWatts int
}

// InverterControl is the brand-neutral way a plugin drives the active
// inverter's battery. The implementation (one driver per inverter family,
// with its register maps) is private and resolved from the inverter profile
// chosen in Settings, so a plugin never deals in Modbus registers and works
// unchanged on every inverter a driver exists for.
//
// Every method is safe to call concurrently. Configure/Enable/Disable
// perform the Modbus writes immediately and return the first error.
type InverterControl interface {
	// Model identifies the inverter currently driven (the active profile's
	// ID). It changes when the user switches inverter in Settings — a
	// plugin should treat anything it armed on the previous one as gone.
	Model() string
	// Available reports whether the inverter can be reached right now.
	Available() bool
	// Supports reports whether the active inverter has a driver for a.
	// Every other method fails (or reports false) for an unsupported action.
	Supports(a BatteryAction) bool
	// Configure writes a's target without switching it on or off. Call it
	// whenever the target changes; an action already running picks it up
	// straight away.
	Configure(a BatteryAction, t BatteryTarget) error
	// Enable switches a on. now is the current time in the installation's
	// timezone — some inverters keep separate settings per day type.
	Enable(a BatteryAction, now time.Time) error
	// Disable switches a off.
	Disable(a BatteryAction) error
	// NeedsRefresh reports whether a, enabled through this InverterControl,
	// has to be enabled again at now to keep running as intended (e.g. the
	// day type the inverter tracks it under changed at midnight).
	NeedsRefresh(a BatteryAction, now time.Time) bool
	// Active reads back from the inverter whether a is currently on,
	// whoever switched it on.
	Active(a BatteryAction) (bool, error)
}
