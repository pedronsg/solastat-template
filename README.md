# solastat-template

Public scaffold and shared framework for solastat plugin submodules (relay,
gridcharge, and any future one). Not itself a deployable plugin — it
provides:

- `proto/` — the `PluginHub` gRPC contract between the core app (e.g.
  `solastat-omie`) and every plugin, plus generated Go code (`proto/gen`).
  The core is a dumb relay for license keys: it stores whatever the user
  pastes into Settings and hands the raw pool to every plugin that asks —
  it never decides authorization itself.
- `pkg/pluginclient` — the client every plugin imports to connect to the
  core's `PluginHub`: solar-data subscription, Modbus register read/write,
  and the key-pool exchange. You supply a `checkAuthorized(keys []string)
  bool` callback (backed by
  [`solastat-auth`](https://github.com/pedronsg/solastat-auth)'s
  `Authorizes`) — pluginclient calls it on every (re)register and fires
  `OnAuthorization` when the verdict changes, so activating a key in
  Settings unlocks a running plugin without a restart.
- `cmd/plugin-template` — a minimal, complete, buildable plugin app,
  including the `solastat-auth`-based verification wiring. Copy this
  directory to start a new plugin: rename the module/package_id, wire
  `pluginclient.OnSnapshot` to your automation logic.

## Starting a new plugin

1. Copy `cmd/plugin-template` into your new plugin repo.
2. Change `pluginID` in `main.go` and `package_id`/`entry_point` in
   `metadata.json` to match.
3. Add this repo and `solastat-auth` as git submodules (same convention as
   `orbit-os-sdk-go`) and point `go.mod`'s `replace` directives at them.
4. Import `github.com/pedronsg/solastat-template/pkg/pluginclient` to talk
   to the core, and `github.com/pedronsg/solastat-auth/pkg/auth` to verify
   your own license key — see `cmd/plugin-template/main.go`.

## License key flow

Verification happens entirely on the plugin side (see `solastat-auth`) —
the core only stores and relays raw key strings, it never parses or
verifies them.

1. Settings page (core app) shows one device hash — `auth.DeviceHash`
   of the device's serial, not tied to any plugin.
2. Whoever holds the private key runs, offline:
   `keygen sign -priv <key.hex> -hash <hash-from-settings> -plugins relay,gridcharge`
   — the key encodes which plugin(s) it unlocks.
3. The resulting key is pasted into Settings' single, generic "activate"
   box. The core just stores it and hands it to every connected plugin on
   their next `Register` call; each plugin checks it against its own ID and
   independently-computed device hash. A plugin nobody has a key for never
   reports itself as authorized, so it never appears in the Settings list.
