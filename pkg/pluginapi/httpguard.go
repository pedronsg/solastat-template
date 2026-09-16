package pluginapi

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

//go:embed not_activated.html
var notActivatedHTML []byte

// notAuthorizedMsg is the one error string every gated plugin endpoint
// returns — centralized here so a new plugin gets identical wording for
// free, instead of every plugin's http.go inventing its own copy.
const notAuthorizedMsg = "plugin not authorized — activate it in Settings"

// RequireAuthorized wraps next so it 403s with a standard JSON error body
// unless authorized() is true at request time. Meant for every route a
// plugin registers — not just the ones that write — so an unlicensed
// plugin does nothing and reveals nothing (current relay/inverter
// readings, saved automation config, activation history, ...), not just
// refuses to change anything. authorized is a func, not a bool, so it's
// always evaluated fresh per request, not just at RegisterRoutes time.
func RequireAuthorized(authorized func() bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": notAuthorizedMsg})
			return
		}
		next(w, r)
	}
}

// ServeDashboard serves activatedHTML while authorized() is true, or a
// shared, generic "this plugin needs a license key" page otherwise — so
// every plugin's unauthorized state looks and behaves identically without
// each one maintaining its own copy of that page, and a brand-new plugin
// gets it for free just by calling this instead of writing its own
// http.HandleFunc for its dashboard route.
func ServeDashboard(authorized func() bool, activatedHTML []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if !authorized() {
			w.Write(notActivatedHTML)
			return
		}
		w.Write(activatedHTML)
	}
}
