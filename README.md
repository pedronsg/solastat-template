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
   `RegisterRoutes(mux, route, apiPrefix string)`.
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
