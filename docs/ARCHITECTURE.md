# Architecture

RDP over Proxy lets Windows' built-in Remote Desktop client (`mstsc.exe`) reach a target computer through a proxy. mstsc has no proxy support, so each connection gets a local tunnel entrance on a loopback address; mstsc connects there, and the tunnel carries the bytes through the chosen proxy.

```
mstsc /v:127.a.b.c:13389 ──▶ tunnel (net.Listen on 127.a.b.c) ──▶ route ──▶ proxy ──▶ target:port
                                                                  │
                                                                  ├─ direct: net.Dialer
                                                                  └─ engine: embedded Xray-core (one instance)
```

Status markers: **[M0]** to **[M3]** are implemented; everything else is planned for the milestone shown. See [PROGRESS.md](PROGRESS.md).

## Principles

- **Native mstsc, launched with `mstsc /v:`.** Since the April 2026 security update, opening an `.rdp` file shows a security dialog every time with all redirections off. Typing an address (or `/v:`) is unaffected, so the app never generates `.rdp` files. Per-connection settings are limited to what the command line supports (`/f /w /h /multimon /span /admin`); clipboard, drives, audio and performance come from the user's `Documents\Default.rdp`.
- **One loopback address per connection** (`127.(1-254).(1-254).(1-254)`, derived from the profile ID and stored in the profile). mstsc remembers credentials and certificate trust per address, so connections never share them.
- **Event-driven.** No sleeps, no fixed retry counts, no arbitrary timeouts. Readiness is "Listen returned"; a session ends when mstsc exits. The documented exceptions are commented where they occur: Xray's `connIdle` is raised to the maximum (an idle cut would drop RDP), display-only timers, and test-harness helpers.
- **Pure core, thin edges.** `model`, `loopback`, `rdpfile`, `sharelink`, the X.224 codec and the session state machine make no OS calls. Windows calls live in `secret`, `mstsc`, `diag`, `winx` and `app`.

## Go packages

| Package | Responsibility | Status |
|---|---|---|
| `main` | Embeds the frontend and icon; holds the default `version` (release builds override it with `-ldflags -X main.version=…`) | [M0] |
| `internal/app` | Wails application, main window, tray, single instance, close-to-tray, startup error box. `desktop_windows.go` and `server.go` split the desktop build from the browser-preview build (`-tags server`) | [M0] |
| `internal/api` | Services bound to the frontend; DTOs; events | [M0] `SettingsService`; more in M4 |
| `internal/model` | Settings [M0]; Proxy, Profile, Target, IDs and field-level validation [M1] | [M1] |
| `internal/store` | Data folders, atomic JSON writes, settings load/repair [M0]; proxies and profiles (M4) | partly |
| `internal/i18n` | Go-side strings (tray, native dialogs), system language detection | [M0] |
| `internal/winx` | Win32 helpers: WebView2 detection, message box, dark-mode query [M0]; a process's main windows, WM_CLOSE, focus [M2] | [M2] |
| `internal/loopback` | Derive and de-duplicate per-profile loopback addresses | [M1] |
| `internal/rdpfile` | Read-only `.rdp` parsing (import, `Default.rdp` RD Gateway check) | [M1] |
| `internal/mstsc` | Build mstsc arguments [M1]; start mstsc, wait, close, kill, focus [M2]; `UsernameHint`; gateway check against `Default.rdp` and policy (M4) | partly |
| `internal/probe` | X.224 Connection Request/Confirm codec and `Check` [M1]; proxy latency test `Latency` [M3] | [M3] |
| `internal/session` | Pure state-machine reducer [M1]; one actor goroutine per session, Manager [M2] | [M2] |
| `internal/tunnel` | Loopback listener, per-connection upstream dial, two-way copy, byte counters, reports | [M2] |
| `internal/route` | `Dialer` and `Provider` interfaces, the direct route [M2] | [M2] |
| `internal/engine` | The single embedded Xray instance and the app's `route.Provider`: outbound registry with ref-counts; forced outbound tag per connection; Xray's failure reason per connection; log bridge [M3]. SOCKS / HTTP now, V2Ray family in M6 | partly |
| `internal/secret` | DPAPI for secrets in JSON; `TERMSRV/<loopback>` credentials in Windows Credential Manager | M4 |
| `internal/sharelink` | Share links (vmess, vless, trojan, ss, hysteria2, socks, http) ⇄ Xray outbound JSON | M6 |
| `internal/diag` | Read-only environment report | M7 |
| `internal/logging` | Log files, in-memory ring buffers, redaction, state-flip de-duplication | M4 |
| `internal/testutil` | Fake RDP server [M1]; helper processes that stand in for mstsc [M2]; `xraytest`: in-process Xray SOCKS / HTTP proxy servers [M3] | [M3] |
| `tools/notices` | Generates `THIRD_PARTY_NOTICES.md` | M9 |

## Data

- `%APPDATA%\RDP-over-proxy\settings.json` [M0], `proxies\<id>.json`, `profiles\<id>.json` (M4)
- `%LOCALAPPDATA%\RDP-over-proxy\WebView2\` [M0], `logs\` (M4)
- `RDP_OVER_PROXY_HOME=<dir>` puts everything under `<dir>\config` and `<dir>\local` (tests, development). The browser-preview build uses `…\RDP-over-proxy-preview`.

Every file carries `"schema": 1`, a data-format number used for migrations (not the app version). Files are decoded on top of the defaults so fields added later get their default value.

**Settings** [M0]: `language` (`zh-CN` | `en`; empty until the first-run picker), `theme` (`system` | `light` | `dark`), `closeBehavior` (`tray` | `quit`), `localPort` (13389), `checkRouteBeforeConnect`, `testUrl`, `logLevel`.

**Proxy** (model [M1], storage M4): `id`, `name`, `kind` (`direct` | `socks` | `http` | `shadowsocks` | `vmess` | `vless` | `trojan` | `hysteria2` | `xray`), `server`, `port`, `username`; `secret` and the full Xray `outbound` JSON (stored as JSON text) are DPAPI-sealed on disk; `summary` (non-secret display fields) arrives with the share-link parser in M6. `direct` is only the built-in entry `DirectProxyID = "direct"`, which every profile can pick and which is never stored.

**Profile** (model [M1], storage M4): `id`, `name`, `group`, `target {host, port}`, `proxyId`, `loopback`, `username`, `rememberPassword`, `display {mode, width, height, multimon, span}`, `admin`. `display.mode` is `default` (no switch, follow `Default.rdp`), `fullscreen` (`/f`, plus `/multimon` or `/span`) or `window` (`/w /h`, 200–8192); the settings of the other modes are kept so switching back restores them. RDP passwords live only in Windows Credential Manager (`CRED_TYPE_GENERIC`, target `TERMSRV/<loopback>`).

IDs are 16 random lowercase hex digits and double as file names. `Validate` on a profile or proxy returns `model.FieldErrors`: every problem at once, each as a JSON field path (`target.host`) plus a code (`required`, `invalid`, `out_of_range`, `too_long`, `unsupported`, `conflict`) that the UI translates. Host names are ASCII (internationalized names in their `xn--` form); the last label cannot be all digits, so `10.0.0.256` is rejected rather than taken for a name.

**Loopback addresses** [M1]: SHA-256 of the profile ID picks a position among the 254³ addresses `127.(1-254).(1-254).(1-254)`; if it is taken, the next free address after it is used (the search covers the range once, so it always ends). The address is stored with the profile, so later changes to the derivation never move existing profiles.

## Frontend ⇄ Go

Wails generates TypeScript bindings for exported service methods (`frontend/bindings`, never edited by hand). Go pushes changes as events; the frontend never polls.

| Service | Methods | Status |
|---|---|---|
| `SettingsService` | `Get`, `Save`, `SystemLanguage`, `AppInfo` | [M0] |
| `ProfileService` | CRUD, import `.rdp`, set/forget password, credential state | M4 |
| `ProxyService` | CRUD, parse/import share links, export link, validate, latency test | M4 / M6 |
| `SessionService` | `CheckRoute(ctx)`, `Connect(ctx)` (cancellable from JS), `Disconnect`, `Focus`, logs | M4 |

Events: `settings:changed` [M0]; `sessions:changed`, `session:log`, `data:changed`, `app:toast` (M4).

## Session lifecycle (reducer [M1], actor M2)

`internal/session` is a pure state machine: `Reduce(state, event)` returns the next state and the effects to perform. The actor (M2) performs them and feeds the outcomes back as events. One step runs at a time, and every step ends in exactly one event: its own success event or `StepFailed`.

| Step | Phase shown | Effect → event | Held afterwards |
|---|---|---|---|
| preflight | preparing | `Preflight` → `PreflightPassed` (RD Gateway settings) | — |
| route | preparing | `AcquireRoute` → `RouteReady` | route |
| listen | preparing | `Listen` → `Listening{Addr}` (ready when Listen returns) | tunnel |
| check (optional) | checking | `RunCheck` → `CheckPassed{Result}` | — |
| credential | launching | `PrepareCredential` → `CredentialReady{OneTime}` (password or `UsernameHint`) | one-time credential |
| launch | launching | `LaunchClient` → `ClientStarted{PID}` (`mstsc /v:<loopback>:<port>` + display switches) | mstsc |
| run | running | until `ClientExited` | |
| done | ended | `DeleteCredential`, `CloseTunnel`, `ReleaseRoute` — only what is held, in that order | nothing |

The route is acquired before listening so the tunnel starts with its dialer; listening comes before the check so an address conflict shows at once; the password is written only after the route has proved itself.

- **Stop before mstsc runs** (phase "ending"): the step in flight finishes — the route check is aborted with `CancelCheck`, the other steps are quick — and then everything held is given back; outcome `cancelled`. If mstsc was starting, it has no window to close yet, so it is killed and the session waits for it to exit.
- **Stop while running**: `CloseClient` posts WM_CLOSE. mstsc may ask the user to confirm, and they may decline, so the session stays "running" until mstsc exits. `Stop{Force}` kills the process (phase "ending") and waits for `ClientExited`; outcome `closed`.
- **A failing step** ends the session at once with `Failure{Step, Err}` and outcome `failed`, unless the user had already asked to stop (then it is `cancelled`).
- **Tunnel reports**: connection count; the upstream state (unknown / ok / failing) is logged only when it flips, while the latest error is kept for display.
- Every log line is a stable message key plus arguments, translated by the UI. Events that don't fit the current step are ignored with a warning line; events after the end are ignored.

**Route check** (`probe.Check`): sends an X.224 Connection Request offering what mstsc offers (`SSL|HYBRID|HYBRID_EX`), with no cookie and no user name, and reads the Connection Confirm. Any well-formed confirm, including a negotiation failure, proves the route and the RDP server; the result carries the elapsed time and the selected protocol. Errors distinguish "closed before answering" (typical of a proxy that could not reach the target), "not RDP" (with the first bytes seen), "truncated" and "malformed". There is no timeout; cancelling the context aborts it.

Connecting a profile that already has a session focuses its window. Only "force" or quitting the app terminates mstsc, and only the PID the app started.

## Runtime [M2]

- **Actor** (`session/actor.go`): one goroutine per session. It performs the effects in order. Each step's result goes back as an event into the session's mailbox, an unbounded queue that never blocks a sender. So the tunnel, the route check and the process-exit waiter can report at any moment, even while the actor is busy closing them. Effects run before the new state is published, so a published "ended" means everything has already been given back. The route check and the wait for mstsc's exit run in their own goroutines; everything else is quick and runs inline.
- **Manager** (`session/manager.go`)
  - Runs at most one session per profile. `Connect` validates the request, then either starts a session or, if one is running, focuses mstsc. If the previous session is still ending, it returns `ErrEnding`.
  - Also provides `Stop(force)`, `Focus`, `States` (the latest state per profile, ended ones included) and `Running` (for "hide to tray while a session runs").
  - `Quit` force-stops every session and returns once each has given everything back.
  - The `Changed` and `Log` callbacks are delivered one at a time and in order, so an old session's last report always arrives before its successor's first.
  - Preflight refuses a direct connection to a loopback target, which would connect the tunnel to itself; more checks plug in through `Options.Preflight` (RD Gateway, M4).
  - Routes come from a `route.Provider`, credentials from `Options.Credentials` (M4).
- **Tunnel**
  - `tunnel.Listen` is ready when it returns. Every accepted connection dials the target through the route and copies both ways with byte counters.
  - The first byte back from the target reports "upstream answered". A dial error, or the route closing the connection before any answer, reports "upstream failed". When mstsc or `Close` ends the connection, nothing is reported.
  - When either direction ends, both are closed. RDP does not half-close, and keeping a half-closed connection would need a timeout.
  - `Close` cancels dials in flight, closes every connection and waits for all goroutines.
- **mstsc process** (`mstsc.Launch`)
  - Starts `%SystemRoot%\System32\mstsc.exe`, never one found on PATH.
  - The app keeps its own handle to the process. Windows does not reuse a PID while a handle is open, so closing (WM_CLOSE to the visible, unowned top-level windows) and focusing can never reach a later process that got the same PID.
  - Kill uses `TerminateProcess` on that handle. Close, focus and kill do nothing once the process has exited.

Tests never start mstsc. Session tests run a real tunnel against the fake RDP server, and the test itself plays mstsc. Launcher tests start a copy of the test binary as a stand-in: either a 1×1 tool window far off-screen, shown without activation, or a windowless process that waits until killed.

## Proxy engine [M3]

The app embeds Xray-core v1.260327.0 as a library: one instance per process, created by `engine.Start` and used as the sessions' `route.Provider`.

- **Base configuration**: no inbounds; one `blackhole` outbound, added first so that it is Xray's default and a connection without a tag goes nowhere; policy level 0 with `connIdle` at its maximum (registered exception: the default 300 s would cut an idle remote desktop).
- **Outbounds**
  - `Acquire(proxy)` turns the proxy into an Xray outbound object (JSON; SOCKS / HTTP are generated from the fields; the V2Ray family arrives in M6) and builds it with Xray's own config code. It is added under a tag of its own.
  - Outbounds are shared by reference count, keyed by proxy ID plus a digest of the outbound. Editing a proxy therefore gives new sessions a new outbound while running sessions keep the old one.
  - The last release removes the outbound from Xray and closes it; Xray's `RemoveHandler` alone would only forget it.
  - The direct entry bypasses Xray entirely (`route.Direct`).
- **Dialing**
  - Every connection is dispatched straight to its outbound with Xray's forced-outbound-tag context, so Xray's routing never decides anything. Domain targets are handed to the proxy unresolved.
  - `core.Dial` returns at once and Xray connects in the background. Each connection carries an error tracker; when the outbound fails, Xray submits its reason before ending the stream, and the connection's `Read` returns that reason in place of a bare EOF. The tunnel and the route check therefore report, for example, "server rejects account", "407 Proxy Authentication Required" or "connection refused".
  - The address is validated before it reaches Xray, which would panic on some malformed destinations.
- **Logging**: Xray's logger is process-wide, and creating an instance installs Xray's own. `Start` replaces it with a bridge to `Options.Log`: errors and warnings always; info and debug only when verbose; access lines (one per connection, naming the target) never.
- **Shutdown order**: `session.Manager.Quit` first (sessions release their routes), then `Engine.Close`.

`probe.Latency` measures one HTTP GET of the test URL through any route: connecting, TLS for https, up to the response headers. Any HTTP status counts and redirects are not followed. There is no timeout; it is cancellable.

Linking Xray adds about 23 MB to the executable (measured with production build flags: 9.9 MB → 32.8 MB for the app without its frontend assets).

## Frontend structure

- `src/app` — shell, theme (follows Windows via `prefers-color-scheme`), first-run language picker [M0]
- `src/features` — connections, proxies, settings [M0 settings], diagnostics
- `src/components` — shared components [M0 `Page`, `EmptyState`]
- `src/stores` — zustand stores fed by service calls and Go events [M0 settings]
- `src/locales` — `zh-CN.json` and `en.json`; a test enforces identical keys [M0]

## Build

`wails3 build` runs `build/Taskfile.yml` and `build/windows/Taskfile.yml`: install frontend deps, generate bindings, `vite build`, generate the icon and the Windows resource (`.syso` from `build/windows/info.json` and `wails.exe.manifest`), then `go build -tags production -trimpath -ldflags "-w -s -H windowsgui"`. Only Windows build files are kept.
