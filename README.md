# solastat-template

Public shared contract between `solastat` (the core) and its plugins
(relay, gridcharge, and any future one — all compiled directly into the
core binary, gated behind `solastat`'s `plugins` Go build tag). There
is no separate process, no RPC, no `.orb` of their own. This repo has no
dependency on the OrbitOS SDK or on anything else: it is the one small,
always-public thing the core requires to build, whether or not the tag is
active.

## `pkg/pluginapi`

```go
type Reading struct { Label string; Value float64; Unit string }

type Hooks interface {
    OnReading(readings map[string]Reading)
    Tick()
}

type Info struct { ID, Version string; Authorized bool }

type LogEvent struct {
    Kind, Text, NotifyType string
    On, OK                 bool
    Code                   int
}

type NotificationType struct {
    ID, Label      string
    DefaultEnabled bool
}

type NotificationTypeProvider interface {
    NotificationTypes() []NotificationType
}

func RequireAuthorized(authorized func() bool, next http.HandlerFunc) http.HandlerFunc
func ServeDashboard(authorized func() bool, activatedHTML []byte) http.HandlerFunc

type BatteryAction string // ForceCharge, ForceExport
type BatteryTarget struct { SocPercent, PowerWatts int }

type InverterControl interface {
    Model() string
    Available() bool
    Supports(a BatteryAction) bool
    Configure(a BatteryAction, t BatteryTarget) error
    Enable(a BatteryAction, now time.Time) error
    Disable(a BatteryAction) error
    NeedsRefresh(a BatteryAction, now time.Time) bool
    Active(a BatteryAction) (bool, error)
}
```

- **`Reading`** — the shared shape for a decoded poll-cycle value. The core
  converts its own `internal/solar.Data` into `map[string]Reading` once per
  poll and hands it to every compiled-in plugin.
- **`Hooks`** — what a plugin exposes. `OnReading` fires once per poll
  cycle; `Tick` fires on a fixed ~1s interval for time-based state
  (timeouts, cooldowns) independent of polling. Both must be safe to call
  even while unauthorized — a plugin gates its own behavior internally
  (see [`solastat-auth`](https://github.com/pedronsg/solastat-auth)) rather
  than trusting the core to withhold calls.
- **`Info`** — what a plugin reports about itself for the Settings page's
  plugin list.
- **`LogEvent`** — one entry a plugin reports for the core's dashboard
  activity log (via the `logEvent func(pluginapi.LogEvent)` closure the
  core hands each plugin at wiring time — see `wire.go`'s `LogEvent`
  field). `Text`/`OK` are the two fields every plugin-sourced entry should
  set: the Logs card renders any non-core source generically off those
  alone (dot green if `OK`, amber otherwise), it never special-cases which
  plugin sent it. `Kind`/`On`/`Code` are free for a plugin's own use (e.g.
  its own history endpoint) and aren't interpreted by the core.
  `NotifyType`+`Text` double as the mobile-notification path: when
  `NotifyType` is non-empty, the core looks up `"<plugin ID>:<NotifyType>"`
  in the user's notification settings and, if enabled, pushes `Text` via
  `MobileNotificationManager`. Leave `NotifyType` empty for log-only events.
- **`NotificationType`** / **`NotificationTypeProvider`** — how a plugin
  declares which of its events are notification-worthy, without the core
  ever hardcoding a plugin's identity. Implement `NotificationTypes()
  []NotificationType` on your `Plugin` (checked via an optional type
  assertion, like `Close() error` already is) and the Settings →
  Notifications page gets a checkbox for it automatically, namespaced
  `"<plugin ID>:<type ID>"` so two plugins can never collide. `NotifyType`
  on a `LogEvent` must match one of the IDs returned here.
- **`RequireAuthorized`** / **`ServeDashboard`** — the shared HTTP guard
  every plugin's `RegisterRoutes` should use for *every* route, GET
  included: an unlicensed plugin should do nothing and reveal nothing
  (current readings, saved config, activation history — not just refuse
  writes), not just look functional until you click Save. `ServeDashboard`
  serves your plugin's real HTML while `authorized()` is true, or a shared,
  generic "not activated" page otherwise, so every plugin's unlicensed
  state looks identical without each one carrying its own copy of that
  page. See relay/gridcharge's `http.go` for the pattern — it's near
  boilerplate-free:

  ```go
  func (p *Plugin) RegisterRoutes(mux *http.ServeMux, route, apiPrefix string) {
      mux.HandleFunc(route, pluginapi.ServeDashboard(p.authorized.Load, dashboardHTML))
      mux.HandleFunc(apiPrefix, pluginapi.RequireAuthorized(p.authorized.Load, func(w http.ResponseWriter, r *http.Request) {
          // ... your actual handler ...
      }))
  }
  ```
- **`InverterControl`** — the brand-neutral way a plugin forces the active
  inverter's battery to charge from the grid (`ForceCharge`) or export to
  it (`ForceExport`). A plugin sets the target with `Configure`, switches
  the action with `Enable`/`Disable`, and reads the real state back with
  `Active`. It never sees a Modbus register: the drivers (one per inverter
  family, picked from the profile chosen in Settings) live in the private
  plugins repo, so a plugin written against this interface works unchanged
  on every inverter a driver exists for. `Supports` says which actions the
  active inverter has a driver for; `Model` changes when the user switches
  inverter.

## The pattern for a new plugin

All plugins live together in one private repo,
[`solastat-plugins`](https://github.com/pedronsg/solastat-plugins) (checked
out as `solastat`'s `plugins/private` submodule):

```
solastat-plugins/
├── wire.go                — the only symbols solastat imports: Wire<Name>(...) helpers
├── pkg/relay/              — a plugin: exported Controller + Plugin satisfying pluginapi.Hooks
├── pkg/gridcharge/         — another one, same shape
├── plugins/auth/           — solastat-auth submodule, for verifying license keys
├── orbit-os-sdk-go/        — vendored SDK copy, for plugins needing device services (GPIO, etc.)
└── go.mod
```

Adding a new plugin:

1. New package `pkg/<name>/` in `solastat-plugins`, same shape as
   `pkg/relay`/`pkg/gridcharge`: a `Controller` for the logic, a `Plugin`
   wrapping it that gates `Hooks`/HTTP writes on
   `solastat-auth.Authorizes(key, deviceHash, PluginID)`, and
   `RegisterRoutes(mux, route, apiPrefix string)` built on
   `pluginapi.RequireAuthorized`/`ServeDashboard` (see above) so every
   route — GET included — is gated the same way with no extra code.
   Optionally implement `NotificationTypes() []pluginapi.NotificationType`
   if any of your `LogEvent`s should be able to push a mobile notification.
2. Add a `Wire<Name>(...)` helper to `wire.go` that constructs it and
   computes the device hash — the one place this repo needs
   `solastat-auth` directly, so `solastat` never has to.
3. In `solastat`, add `wire<Name>(...)` to `cmd/solastat/plugins.go`
   (`//go:build plugins`) calling `plugins.Wire<Name>(...)`, and a matching
   no-op in `plugins_stub.go` (`//go:build !plugins`) so the untagged build
   always compiles. Wire it into `main.go` the same way relay/gridcharge
   are: call `OnReading`/`Tick` from the existing hooks, `RegisterRoutes`
   on the shared mux, `registerPlugin(...)` so it shows up in Settings.
4. `solastat/go.mod` needs `require`+`replace` entries for
   `solastat-plugins` (and `solastat-auth`, since a replace directive
   inside a dependency is ignored when that dependency is used by another
   module) — always safe to have present even when the submodules aren't
   checked out: Go's lazy module loading never resolves an unused
   `replace` target, so the public core still builds standalone with just
   this repo. Confirmed by building a fresh clone with only
   `plugins/template` initialized.
5. Building with `-tags plugins` (and `plugins/private` checked out)
   compiles every plugin directly into the `solastat` binary — one
   process, one `.orb`. Building without the tag (the default) produces
   the plain core, with no reference to any of them.

## License activation

Unchanged in spirit from the previous gRPC-based design, just simpler now
that there's no process boundary: the core's Settings page shows one device
hash (`SHA256(deviceSerial)`, reimplemented locally in the core — see
`internal/pluginhub` in `solastat` — so it never needs to import
`solastat-auth` itself) and one generic "paste a key" box. The core just
stores whatever key is pasted; each compiled-in plugin's wiring code
verifies it directly (`solastat-auth.Authorizes(key, hash, pluginID)`)
against the core's stored key pool and gates its own `Hooks` methods
accordingly — the core never parses or verifies a key itself.
