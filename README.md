# solastat-template

Public scaffold and shared framework for solastat plugin submodules (relay,
gridcharge, auth, and any future one). Not itself a deployable plugin — it
provides:

- `proto/` — the `PluginHub` gRPC contract between the core app (e.g.
  `solastat-omie`) and every plugin, plus generated Go code (`proto/gen`).
- `pkg/license` — the offline Ed25519 scheme used to unlock a plugin on a
  specific device: `licensegen` signs `SHA256(deviceSerial+":"+pluginID)`
  offline, the core verifies it with the embedded public key.
- `pkg/pluginclient` — the client every plugin imports to connect to the
  core's `PluginHub`: authorization handshake (re-checked periodically, no
  restart needed after activation), solar-data subscription, and Modbus
  register read/write. The plugin never handles a license key itself — it
  only asks the core "am I authorized right now".
- `cmd/licensegen` — offline-only CLI to generate the signing key pair and
  sign per-device/per-plugin license keys. Never runs on a device.
- `cmd/plugin-template` — a minimal, complete, buildable plugin app. Copy
  this directory to start a new plugin: rename the module/package_id, wire
  `pluginclient.OnSnapshot` to your automation logic.

## Starting a new plugin

1. Copy `cmd/plugin-template` into your new plugin repo.
2. Change `pluginID` in `main.go` and `package_id`/`entry_point` in
   `metadata.json` to match.
3. Import `github.com/pedronsg/solastat-template/pkg/pluginclient` to talk
   to the core, and `pkg/license` if you need to verify licenses yourself.
4. Add this repo as a git submodule (or vendor its `orbit-os-sdk-go` copy the
   same way) and point `go.mod`'s `replace` at it.

## License key flow

The entire hash → key exchange happens inside the core app's Settings page —
plugins are unaware of it, they just ask "am I authorized" via `Register`.

1. Settings page (core app) shows `SHA256(deviceSerial+":"+pluginID)` for a
   locked plugin.
2. Whoever holds the private key runs, offline:
   `licensegen sign -priv <key.hex> -hash <hash-from-settings>`
3. The resulting key is pasted back into Settings. The core verifies it
   itself (it holds the public key) and persists the result; the plugin
   picks up authorization within `pluginclient.reregisterInterval`, no
   restart needed.
