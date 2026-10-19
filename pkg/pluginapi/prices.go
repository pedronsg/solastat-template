package pluginapi

import "time"

// PriceSlot is one day-ahead market price, for delivery over
// [Start, Start+Minutes).
type PriceSlot struct {
	Start   time.Time `json:"start"`
	Minutes int       `json:"minutes"`
	EURMWh  float64   `json:"eur_mwh"`
}

// Prices is what a PriceProvider holds: today's prices and, once the
// market has published them, tomorrow's. Slots are in time order; a period
// the market didn't price (e.g. OMIE's Portuguese file has no 23:00-24:00
// in Lisbon time) is simply missing.
type Prices struct {
	Source string      `json:"source"` // the market's name, e.g. "OMIE"
	Slots  []PriceSlot `json:"slots"`
}

// PriceProvider is a plugin that fetches day-ahead market prices and shares
// them with the other plugins. Prices is false while the provider can't
// share any (e.g. it isn't licensed on this device). Asking also tells the
// provider that someone needs the prices, so it fetches them even when its
// own automation wouldn't.
type PriceProvider interface {
	Prices() (Prices, bool)
}
