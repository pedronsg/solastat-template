# solastat-template

Public shared contract for solastat plugin submodules (relay, gridcharge,
and any future one). Every plugin is a plain Go library, compiled directly
into the `solastat-omie` core binary via a Go build tag when its submodule
is checked out — there is no separate process, no RPC, no `.orb` of its
own. This repo has no dependency on the OrbitOS SDK or on anything else: it
is the one small, always-public thing the core requires to build.

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

A plugin repo (see `solastat-relay` for a full example) looks like:

```
solastat-relay/
├── pkg/relay/            — the plugin: exported Controller satisfying pluginapi.Hooks
├── plugins/auth/          — solastat-auth submodule, for verifying its own license key
├── orbit-os-sdk-go/        — vendored SDK copy, only if the plugin needs device services (GPIO, etc.)
└── go.mod
```

In `solastat-omie`:

1. Add the plugin as a git submodule under `plugins/<name>/`.
2. Add `require`+`replace` entries to `solastat-omie/go.mod` pointing at it
   (and at `solastat-auth`, if the plugin needs it) — this is always safe to
   have present even when the submodule isn't checked out: Go's lazy module
   loading never resolves an unused `replace` target, so the public core
   still builds standalone with just this repo. Confirmed by building a
   fresh clone with only `plugins/template` initialized.
3. Add a build-tag-gated wiring file, e.g. `cmd/solastat-omie/plugins_relay.go`
   with `//go:build relay` at the top, that imports the plugin package,
   constructs it, and registers its HTTP routes on the core's `*http.ServeMux`.
   Pair it with a `//go:build !relay` stub of the same function signature
   that does nothing, so the untagged build always compiles.
4. Building with `-tags relay` (and the submodule checked out) compiles the
   plugin directly into the `solastat-omie` binary — one process, one
   `.orb`. Building without the tag (the default) produces the plain core,
   with no reference to the plugin at all.

## License activation

Unchanged in spirit from the previous gRPC-based design, just simpler now
that there's no process boundary: the core's Settings page shows one device
hash (`SHA256(deviceSerial)`, reimplemented locally in the core — see
`internal/pluginhub` in `solastat-omie` — so it never needs to import
`solastat-auth` itself) and one generic "paste a key" box. The core just
stores whatever key is pasted; each compiled-in plugin's wiring code
verifies it directly (`solastat-auth.Authorizes(key, hash, pluginID)`)
against the core's stored key pool and gates its own `Hooks` methods
accordingly — the core never parses or verifies a key itself.
