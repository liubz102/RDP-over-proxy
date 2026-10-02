# Architecture

RDP over Proxy lets Windows' built-in Remote Desktop client (`mstsc.exe`) reach a target computer through a proxy. mstsc has no proxy support, so each connection gets a local tunnel entrance on a loopback address; mstsc connects there, and the tunnel carries the bytes through the chosen proxy.

```
mstsc /v:127.a.b.c:13389 ──▶ tunnel (net.Listen on 127.a.b.c) ──▶ route ──▶ proxy ──▶ target:port
                                                                  │
                                                                  ├─ direct: net.Dialer
                                                                  └─ engine: embedded Xray-core (one instance)
```

Status markers: **[M0]** is implemented; everything else is planned for the milestone shown. See [PROGRESS.md](PROGRESS.md).

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
| `internal/model` | Settings [M0]; Proxy, Profile and validation (M1) | partly |
| `internal/store` | Data folders, atomic JSON writes, settings load/repair [M0]; proxies and profiles (M4) | partly |
| `internal/i18n` | Go-side strings (tray, native dialogs), system language detection | [M0] |
| `internal/winx` | Win32 helpers: WebView2 detection, message box, dark-mode query [M0]; focus a window by PID, WM_CLOSE (M2) | partly |
| `internal/loopback` | Derive and de-duplicate per-profile loopback addresses | M1 |
| `internal/rdpfile` | Read-only `.rdp` parsing (import, `Default.rdp` RD Gateway check) | M1 |
| `internal/mstsc` | Build mstsc arguments; `UsernameHint`; gateway check against `Default.rdp` and policy | M1 / M4 |
| `internal/probe` | X.224 Connection Request/Confirm codec, `CheckRDP`, proxy latency test | M1 / M3 |
| `internal/session` | Pure state-machine reducer, one actor goroutine per session, Manager | M1 / M2 |
| `internal/tunnel` | Loopback listener, per-connection upstream dial, two-way copy, byte counters, events | M2 |
| `internal/route` | `Dialer` interface: direct or engine | M3 |
| `internal/engine` | The single embedded Xray instance; outbound registry with ref-counts; forced outbound tag per connection; connection error events; log bridge | M3 |
| `internal/secret` | DPAPI for secrets in JSON; `TERMSRV/<loopback>` credentials in Windows Credential Manager | M4 |
| `internal/sharelink` | Share links (vmess, vless, trojan, ss, hysteria2, socks, http) ⇄ Xray outbound JSON | M6 |
| `internal/diag` | Read-only environment report | M7 |
| `internal/logging` | Log files, in-memory ring buffers, redaction, state-flip de-duplication | M4 |
| `internal/testutil` | Fake RDP server, in-process Xray servers | M1 / M3 |
| `tools/notices` | Generates `THIRD_PARTY_NOTICES.md` | M9 |

## Data

- `%APPDATA%\RDP-over-proxy\settings.json` [M0], `proxies\<id>.json`, `profiles\<id>.json` (M4)
- `%LOCALAPPDATA%\RDP-over-proxy\WebView2\` [M0], `logs\` (M4)
- `RDP_OVER_PROXY_HOME=<dir>` puts everything under `<dir>\config` and `<dir>\local` (tests, development). The browser-preview build uses `…\RDP-over-proxy-preview`.

Every file carries `"schema": 1`, a data-format number used for migrations (not the app version). Files are decoded on top of the defaults so fields added later get their default value.

**Settings** [M0]: `language` (`zh-CN` | `en`; empty until the first-run picker), `theme` (`system` | `light` | `dark`), `closeBehavior` (`tray` | `quit`), `localPort` (13389), `checkRouteBeforeConnect`, `testUrl`, `logLevel`.

**Proxy** (M1/M4): `id`, `name`, `kind` (`direct` | `socks` | `http` | `shadowsocks` | `vmess` | `vless` | `trojan` | `hysteria2` | `xray`), `server`, `port`, `username`; `secret` and the full Xray `outbound` JSON are DPAPI-sealed; `summary` holds non-secret display fields.

**Profile** (M1/M4): `id`, `name`, `group`, `target {host, port}`, `proxyId`, `loopback`, `username`, `rememberPassword`, `display {mode, width, height, multimon, span}`, `admin`. RDP passwords live only in Windows Credential Manager (`CRED_TYPE_GENERIC`, target `TERMSRV/<loopback>`).

## Frontend ⇄ Go

Wails generates TypeScript bindings for exported service methods (`frontend/bindings`, never edited by hand). Go pushes changes as events; the frontend never polls.

| Service | Methods | Status |
|---|---|---|
| `SettingsService` | `Get`, `Save`, `SystemLanguage`, `AppInfo` | [M0] |
| `ProfileService` | CRUD, import `.rdp`, set/forget password, credential state | M4 |
| `ProxyService` | CRUD, parse/import share links, export link, validate, latency test | M4 / M6 |
| `SessionService` | `CheckRoute(ctx)`, `Connect(ctx)` (cancellable from JS), `Disconnect`, `Focus`, logs | M4 |

Events: `settings:changed` [M0]; `sessions:changed`, `session:log`, `data:changed`, `app:toast` (M4).

## Session lifecycle (M1–M2)

1. **Checking** (optional, cancellable): an X.224 Connection Request goes through the proxy to the target; the Connection Confirm reports latency and the security protocols the target accepts.
2. **Preparing**: RD Gateway check; acquire the route; listen on the loopback (ready when Listen returns); write the credential or `UsernameHint` as configured.
3. **Launching**: `mstsc /v:<loopback>:<port>` plus display flags.
4. **Running**: count connections; the upstream state flips between unknown, ok and failing, and is logged only on a flip.
5. **Ending** (mstsc exited): close the listener, cancel connections, delete a one-time credential, release the route.

Connecting a profile that already has a session focuses its window. Disconnect posts WM_CLOSE; only "force" or quitting the app terminates the process, and only the PID the app started.

## Frontend structure

- `src/app` — shell, theme (follows Windows via `prefers-color-scheme`), first-run language picker [M0]
- `src/features` — connections, proxies, settings [M0 settings], diagnostics
- `src/components` — shared components [M0 `Page`, `EmptyState`]
- `src/stores` — zustand stores fed by service calls and Go events [M0 settings]
- `src/locales` — `zh-CN.json` and `en.json`; a test enforces identical keys [M0]

## Build

`wails3 build` runs `build/Taskfile.yml` and `build/windows/Taskfile.yml`: install frontend deps, generate bindings, `vite build`, generate the icon and the Windows resource (`.syso` from `build/windows/info.json` and `wails.exe.manifest`), then `go build -tags production -trimpath -ldflags "-w -s -H windowsgui"`. Only Windows build files are kept.
