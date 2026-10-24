package pluginapi

import "time"

// ForecastPoint is the solar power a forecast expects at Time, W.
type ForecastPoint struct {
	Time  time.Time `json:"time"`
	Watts float64   `json:"watts"`
}

// ForecastDay is the solar energy a forecast expects over one local day.
type ForecastDay struct {
	Day time.Time `json:"day"` // its local midnight
	KWh float64   `json:"kwh"`
}

// Forecast is what a ForecastProvider holds: the expected power through
// today and tomorrow (Points, in time order) and each day's total.
type Forecast struct {
	Source string          `json:"source"` // e.g. "Forecast.Solar"
	Points []ForecastPoint `json:"points"`
	Days   []ForecastDay   `json:"days"`
}

// ForecastProvider is a plugin that holds a solar production forecast for
// this installation and shares it, for a screen on the device (the Pro's
// LCD). Forecast is false while it can't share one (not licensed, or the
// forecast not set up); true with no points while it has none yet. Asking
// also tells the provider that someone needs it, so it fetches it even when
// its own automation wouldn't.
type ForecastProvider interface {
	Forecast() (Forecast, bool)
}
