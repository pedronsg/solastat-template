package pluginapi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
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
//
// Each page has an ETag, with no-cache: the browser asks every time (the
// page must match the core's /i18n.js and /nav.js after an update) and a
// copy it already has costs a 304, not the whole page.
func ServeDashboard(authorized func() bool, activatedHTML []byte) http.HandlerFunc {
	activatedTag, notActivatedTag := etagOf(activatedHTML), etagOf(notActivatedHTML)
	return func(w http.ResponseWriter, r *http.Request) {
		body, etag := activatedHTML, activatedTag
		if !authorized() {
			body, etag = notActivatedHTML, notActivatedTag
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", etag)
		if noneMatch(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write(body)
	}
}

// etagOf is body's ETag, quoted.
func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// noneMatch reports whether an If-None-Match header names etag.
func noneMatch(inm, etag string) bool {
	for _, t := range strings.Split(inm, ",") {
		if t = strings.TrimPrefix(strings.TrimSpace(t), "W/"); t == etag || t == "*" {
			return true
		}
	}
	return false
}
