# Architecture

RDP over Proxy lets Windows' built-in Remote Desktop client (`mstsc.exe`) reach a target computer through a proxy. mstsc has no proxy support, so each connection gets a local tunnel entrance on a loopback address; mstsc connects there, and the tunnel carries the bytes through the chosen proxy.

```
mstsc /v:127.a.b.c:13389 ──▶ tunnel (net.Listen on 127.a.b.c) ──▶ route ──▶ proxy ──▶ target:port
                                                                  │
                                                                  ├─ direct: net.Dialer
                                                                  └─ engine: embedded Xray-core (one instance)
```

Status markers: **[M0]** to **[M7]** are implemented; everything else is planned for the milestone shown. See [PROGRESS.md](PROGRESS.md).

## Principles

- **Native mstsc, launched with `mstsc /v:`.** Since the April 2026 security update, opening an `.rdp` file shows a security dialog every time with all redirections off. Typing an address (or `/v:`) is unaffected, so the app never generates `.rdp` files. Per-connection settings are limited to what the command line supports (`/f /w /h /multimon /span /admin`); clipboard, drives, audio and performance come from the user's `Documents\Default.rdp`.
- **One loopback address per connection** (`127.(1-254).(1-254).(1-254)`, derived from the profile ID and stored in the profile). mstsc remembers credentials and certificate trust per address, so connections never share them.
- **Event-driven.** No sleeps, no fixed retry counts, no arbitrary timeouts. Readiness is "Listen returned"; a session ends when mstsc exits. The documented exceptions are commented where they occur: Xray's `connIdle` is raised to the maximum (an idle cut would drop RDP), display-only timers, and test-harness helpers.
- **Pure core, thin edges.** `model`, `loopback`, `rdpfile`, `sharelink`, the X.224 codec and the session state machine make no OS calls. Windows calls live in `secret`, `mstsc`, `diag`, `winx` and `app`.

## Go packages

| Package | Responsibility | Status |
|---|---|---|
| `main` | Embeds the frontend and the icon (`build/windows/icon.ico`, for the tray); holds the default `version` (release builds override it with `-ldflags -X main.version=…`) | [M0] |
| `internal/app` | Wails application, main window, tray, single instance, close-to-tray, startup error box. `desktop_windows.go` and `server.go` split the desktop build from the browser-preview build (`-tags server`) | [M0] |
| `internal/api` | Services bound to the frontend, their shared `Core`, views (DTOs), events, error JSON | [M4] |
| `internal/model` | Settings [M0]; Proxy, Profile, Target, IDs and field-level validation [M1]; the V2Ray family's settings (`ProxyOptions`), their defaults and checks [M6] | [M6] |
| `internal/store` | Data folders, atomic JSON writes, settings load/repair [M0]; proxies and profiles (`Data`) [M4] | [M4] |
| `internal/i18n` | Go-side strings (tray, native dialogs), system language detection | [M0] |
| `internal/winx` | Win32 helpers: WebView2 detection, message box, dark-mode query [M0]; a process's main windows, their class and enabled state, WM_CLOSE, bringing a window to the front [M2, M5]; file versions, whether Credential Guard runs (WMI `Win32_DeviceGuard`), opening a folder, the WebView2 version [M7] | [M7] |
| `internal/loopback` | Derive and de-duplicate per-profile loopback addresses | [M1] |
| `internal/rdpfile` | Read-only `.rdp` parsing (import, `Default.rdp` RD Gateway check) [M1]; "authentication level" and "prompt for credentials", `MaxSize` for imports [M7] | [M7] |
| `internal/mstsc` | Build mstsc arguments [M1]; start mstsc, wait, close, kill, focus [M2]; per-server memory (`UsernameHint`, forget), RD Gateway check against `Default.rdp` and Group Policy [M4]; `ReadDefaults` (what `Default.rdp` and the gateway policy decide for every connection), `EditDefaults` (`mstsc /edit Default.rdp`) [M7] | [M7] |
| `internal/probe` | X.224 Connection Request/Confirm codec and `Check` [M1]; proxy latency test `Latency` [M3] | [M3] |
| `internal/session` | Pure state-machine reducer [M1]; one actor goroutine per session, Manager [M2] | [M2] |
| `internal/tunnel` | Loopback listener, per-connection upstream dial, two-way copy, byte counters, reports | [M2] |
| `internal/route` | `Dialer` and `Provider` interfaces, the direct route [M2] | [M2] |
| `internal/engine` | The single embedded Xray instance and the app's `route.Provider`: outbound registry with ref-counts; forced outbound tag per connection; Xray's failure reason per connection; log bridge [M3]. The outbound of every kind, and `Check` before saving [M6] | [M6] |
| `internal/secret` | DPAPI for secrets in JSON; `TERMSRV/<loopback>` credentials in Windows Credential Manager | [M4] |
| `internal/sharelink` | Share links (vmess, vless, trojan, ss, hysteria2, socks, http) ⇄ `model.Proxy` | [M6] |
| `internal/diag` | Read-only environment report: `Gather` reads Windows, `Build` (pure) turns the facts into report items; the CredSSP saved-credential delegation verdict | [M7] |
| `internal/logging` | Log file with size-based rotation, in-memory rings, redaction, repeat collapsing, slog bridge for the Wails runtime | [M4] |
| `internal/errcode` | Stable error codes the UI translates; picks the most useful code in an error tree | [M4] |
| `tests/winx`, `tests/diag` | Tests that read this Windows (file versions, WMI, `diag.Gather`) next to the pure ones [M7] | [M7] |
| `tests/testutil` | Fake RDP server [M1]; helper processes that stand in for mstsc [M2]; `xraytest`: in-process Xray SOCKS / HTTP proxy servers [M3], and servers of the V2Ray family over each transport, with TLS or REALITY [M6]. Moved out of `internal/` with the tests [M5] | [M6] |
| `tools/notices` | Generates `THIRD_PARTY_NOTICES.md` | M9 |

## Data

- `%APPDATA%\RDP-over-proxy\settings.json` [M0], `proxies\<id>.json`, `profiles\<id>.json` [M4]
- `%LOCALAPPDATA%\RDP-over-proxy\WebView2\` [M0], `logs\app.log` [M4]
- `RDP_OVER_PROXY_HOME=<dir>` puts everything under `<dir>\config` and `<dir>\local` (tests, development). The browser-preview build uses `…\RDP-over-proxy-preview`.

Every file carries `"schema": 1`, a data-format number used for migrations (not the app version). Files are decoded on top of the defaults so fields added later get their default value.

Loading proxies and profiles (`store.OpenData`) [M4] never stops the app; each problem becomes a notice:
- The file name is the ID; an ID inside the file is ignored.
- Invalid JSON is renamed to `<name>.corrupt` and left out. A file with a newer `schema`, or one that cannot be opened at all (held by another program, no access), is left untouched and left out.
- Secrets that DPAPI cannot open (a file from another Windows user or computer) are cleared; the proxy still loads.
- A missing, malformed or duplicate loopback address is replaced (the lower ID keeps a contested one) and saved at once.
- Temporary files left by an interrupted atomic write are removed.

**Settings** [M0]: `language` (`zh-CN` | `en`; empty until the first-run picker), `theme` (`system` | `light` | `dark`), `closeBehavior` (`tray` | `quit`), `localPort` (13389), `checkRouteBeforeConnect`, `testUrl`, `logLevel`.

**Proxy** (model [M1], storage [M4], V2Ray family [M6]): `id`, `name`, `kind` (`direct` | `socks` | `http` | `shadowsocks` | `vmess` | `vless` | `trojan` | `hysteria2` | `xray`), `server`, `port`, `username` (SOCKS5 / HTTP), `secret`, `options`, `outbound`. `direct` is only the built-in entry `DirectProxyID = "direct"`, which every profile can pick and which is never stored.
- `secret` is what signs in: a password, or the user ID of VMess and VLESS.
- `options` (`model.ProxyOptions`) [M6] are the V2Ray family's settings, named after the share-link parameters: the protocol's own (Shadowsocks method, VMess cipher, VLESS flow and encryption, Hysteria2's Salamander password), the network (`tcp` with an optional HTTP disguise, `ws`, `grpc`, `xhttp`, `httpupgrade`, `kcp` with a disguise and seed) and the security layer (`none`, `tls` with SNI, ALPN, fingerprint, pinned certificate hashes, names to verify and ECH; `reality` with SNI, fingerprint, public key, short ID, spiderX and ML-DSA-65 key), plus Xray's `finalmask` and XHTTP's `extra` as JSON. `Normalize` keeps only the options the kind, network and security use and fills in the defaults, so what is stored is what is used.
- `outbound` is a custom (`xray`) proxy's complete Xray outbound, JSON text.
- On disk `secret`, `options` (as JSON text) and `outbound` are DPAPI-sealed: the options hold obfuscation keys and the paths that lead to the server. The list (`ProxyView`) carries none of the three; it gets `network` and `security` for display (`Proxy.Transport`, which reads a custom outbound's `streamSettings` too).
- When the sealed values cannot be opened (a file from another Windows user or computer), none of them is used: what is left are defaults, which are no way to reach the server (a user ID would go out without the TLS it was meant to travel in). `Data.SecretsLost` marks such a proxy until it is saved again; the list shows it (`ProxyView.secretsLost`), and connecting through it or testing it is refused (`proxy.secretsLost`).
- Trojan defaults to TLS; the other kinds start without a security layer, as share links do.
- HTTP/2 and QUIC transports, the old XTLS and `allowInsecure` are gone from Xray, so they are refused or noted (see Share links).

**Profile** (model [M1], storage [M4]): `id`, `name`, `group`, `target {host, port}`, `proxyId`, `loopback`, `username`, `rememberPassword`, `display {mode, width, height, multimon, span}`, `admin`. `display.mode` is `default` (no switch, follow `Default.rdp`), `fullscreen` (`/f`, plus `/multimon` or `/span`) or `window` (`/w /h`, 200–8192); the settings of the other modes are kept so switching back restores them. RDP passwords live only in Windows Credential Manager (`CRED_TYPE_GENERIC`, target `TERMSRV/<loopback>` without the port, as tools that pre-store mstsc passwords write it). A remembered password persists on this computer; a one-time password persists for the Windows logon session only and is deleted when its session ends. mstsc's own "Remember me" stores a domain-password credential under the same name; the app reports and deletes it but never writes one.

IDs are 16 random lowercase hex digits and double as file names. `Validate` on a profile or proxy returns `model.FieldErrors`: every problem at once, each as a JSON field path (`target.host`) plus a code (`required`, `invalid`, `out_of_range`, `too_long`, `unsupported`, `conflict`) that the UI translates. Host names are ASCII (internationalized names in their `xn--` form); the last label cannot be all digits, so `10.0.0.256` is rejected rather than taken for a name.

**Loopback addresses** [M1]: SHA-256 of the profile ID picks a position among the 254³ addresses `127.(1-254).(1-254).(1-254)`; if it is taken, the next free address after it is used (the search covers the range once, so it always ends). The address is stored with the profile, so later changes to the derivation never move existing profiles.

## Frontend ⇄ Go

Wails generates TypeScript bindings for exported service methods (`frontend/bindings`, never edited by hand). Go pushes changes as events; the frontend never polls.

| Service | Methods | Status |
|---|---|---|
| `SettingsService` | `Get`, `Save`, `SystemLanguage`, `AppInfo` | [M0] |
| `ProfileService` | `List` (with password state), `Draft`, `Create(profile, password)`, `Update(profile, password)`, `Delete`, `ForgetPassword` [M4]; `SetProxy(profile, proxy)` (`Update` keeps the stored proxy) [M6]; `ParseRDP(fileName, data)` [M7] | [M7] |
| `ProxyService` | `List` (no secrets), `Get`, `Create`, `Update(proxy, keepSecret)`, `Latency(ctx)` [M4]; `ParseLink`, `ShareLink`, `DraftLatency(ctx, proxy, keepSecret)`, `Delete(proxy, moveToDirect)` [M6] | [M6] |
| `SessionService` | `Connect(profile, password)`, `Disconnect(profile, force)`, `Focus`, `States`, `Log`, `CheckRoute(ctx)` | [M4] |
| `AppService` | `Notices`, `Dismiss`, `Log` [M4]; `Quit(confirmed)`, `KeepRunning` [M5] | [M5] |
| `DiagService` | `Report`, `EditDefaults`, `OpenLogs` | [M7] |

Methods that take a `context.Context` are cancellable from JavaScript (cancel the returned promise). `Connect` returns at once; progress arrives as events, and `Disconnect` is the way to stop.

Events: `settings:changed` [M0]; `data:changed` (every profile and proxy), `sessions:changed` (one session's state), `session:log` (one log line), `app:notice` [M4]; `app:quitRequested` [M5]; `diag:changed` (the environment report, once Credential Guard is known) [M7].

**Errors** [M4]: a service error reaches the frontend as the rejected call's `cause`: `{code, message, fields?, args?}`. The UI shows the translation of `errors.<code>` with `args` filled in, keeps `message` as the details, and marks `fields` (validation errors, code `validation`). A test in `internal/app` checks that every declared code, notice code and session log key has a translation in both catalogs.

## Session lifecycle (reducer [M1], actor M2)

`internal/session` is a pure state machine: `Reduce(state, event)` returns the next state and the effects to perform. The actor (M2) performs them and feeds the outcomes back as events. One step runs at a time, and every step ends in exactly one event: its own success event or `StepFailed`.

| Step | Phase shown | Effect → event | Held afterwards |
|---|---|---|---|
| preflight | preparing | `Preflight` → `PreflightPassed` (no direct connection to this computer; RD Gateway check [M4]) | — |
| route | preparing | `AcquireRoute` → `RouteReady` | route |
| listen | preparing | `Listen` → `Listening{Addr}` (ready when Listen returns) | tunnel |
| check (optional) | checking | `RunCheck` → `CheckPassed{Result}` | — |
| credential | launching | `PrepareCredential` → `CredentialReady{OneTime}` (password or `UsernameHint`) | one-time credential |
| launch | launching | `LaunchClient` → `ClientStarted{PID}` (`mstsc /v:<loopback>:<port>` + display switches) | mstsc |
| run | running | until `ClientExited` | |
| done | ended | `DeleteCredential`, `CloseTunnel`, `ReleaseRoute` — only what is held, in that order | nothing |

The route is acquired before listening so the tunnel starts with its dialer; listening comes before the check so an address conflict shows at once; the password is written only after the route has proved itself.

- **Stop before mstsc runs** (phase "ending"): the step in flight finishes — the route check is aborted with `CancelCheck`, the other steps are quick — and then everything held is given back; outcome `cancelled`. If mstsc was starting, it has no window to close yet, so it is killed and the session waits for it to exit.
- **Stop while running**: `CloseClient` posts WM_CLOSE to mstsc's remote session window. mstsc asks the user to confirm, and they may decline, so the session stays "running" until mstsc exits. When there is no session window that could ask (mstsc is still connecting or asking for a password, or a dialog of its own disables the window), the actor reports `NothingToClose` and the session kills mstsc [M5]: closing the credential prompt instead left mstsc running with no window at all (seen on a real machine). `Stop{Force}` kills the process (phase "ending") and waits for `ClientExited`; outcome `closed`.
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
  - Preflight refuses a direct connection to a loopback target, which would connect the tunnel to itself; more checks plug in through `Options.Preflight` (the RD Gateway check [M4]).
  - Routes come from a `route.Provider`, credentials from `Options.Credentials` [M4].
  - `Active(profile)` is true from `Connect` until the session has ended, before its first report too; deleting a profile checks it.
- **Tunnel**
  - `tunnel.Listen` is ready when it returns. Every accepted connection dials the target through the route and copies both ways with byte counters.
  - The first byte back from the target reports "upstream answered". A dial error, or the route closing the connection before any answer, reports "upstream failed". When mstsc or `Close` ends the connection, nothing is reported.
  - When either direction ends, both are closed. RDP does not half-close, and keeping a half-closed connection would need a timeout.
  - `Close` cancels dials in flight, closes every connection and waits for all goroutines.
- **mstsc process** (`mstsc.Launch`)
  - Starts `%SystemRoot%\System32\mstsc.exe`, never one found on PATH.
  - The app keeps its own handle to the process. Windows does not reuse a PID while a handle is open, so closing and focusing can never reach a later process that got the same PID.
  - Close [M5] posts WM_CLOSE only to the remote session window: a visible, unowned, enabled top-level window of class `TscShellContainerClass` (the class mstsc has used since Windows XP). It reports whether there was one. Focus brings that window forward, or else whatever mstsc shows (the credential prompt).
  - Kill uses `TerminateProcess` on that handle. Close, focus and kill do nothing once the process has exited.

Tests never start mstsc. Session tests run a real tunnel against the fake RDP server, and the test itself plays mstsc. Launcher tests start a copy of the test binary as a stand-in: either a 1×1 tool window far off-screen, shown without activation, or a windowless process that waits until killed.

## Proxy engine [M3]

The app embeds Xray-core v1.260327.0 as a library: one instance per process, created by `engine.Start` and used as the sessions' `route.Provider`.

- **Base configuration**: no inbounds; one `blackhole` outbound, added first so that it is Xray's default and a connection without a tag goes nowhere; policy level 0 with `connIdle` at its maximum (registered exception: the default 300 s would cut an idle remote desktop).
- **Outbounds**
  - `Acquire(proxy)` turns the proxy into an Xray outbound object (JSON generated from the fields and options [M6]; a custom outbound as written) and builds it with Xray's own config code. It is added under a tag of its own.
  - Outbounds are shared by reference count, keyed by proxy ID plus a digest of the outbound. Editing a proxy therefore gives new sessions a new outbound while running sessions keep the old one.
  - The last release removes the outbound from Xray and closes it; Xray's `RemoveHandler` alone would only forget it.
  - The direct entry bypasses Xray entirely (`route.Direct`).
- **Dialing**
  - Every connection is dispatched straight to its outbound with Xray's forced-outbound-tag context, so Xray's routing never decides anything. Domain targets are handed to the proxy unresolved.
  - `core.Dial` returns at once and Xray connects in the background. Each connection carries an error tracker; when the outbound fails, Xray submits its reason before ending the stream, and the connection's `Read` returns that reason in place of a bare EOF. The tunnel and the route check therefore report, for example, "server rejects account", "407 Proxy Authentication Required" or "connection refused".
  - The address is validated before it reaches Xray, which would panic on some malformed destinations.
- **Logging**: Xray's logger is process-wide, and creating an instance installs Xray's own. `Start` replaces it with a bridge to `Options.Log`: errors and warnings always; info and debug only while `Options.Verbose()` reports true (the app passes "the log level is debug"); access lines (one per connection, naming the target) never. Xray's start-up line, which it logs as a warning so that it shows by default, is passed on as information.
- **Shutdown order**: `session.Manager.Quit` first (sessions release their routes), then `Engine.Close`.

**The V2Ray family** [M6] (`engine.Outbound`, from `model.ProxyOptions`; checked end to end against real Xray servers in `tests/engine`):
- VMess, VLESS, Trojan and Shadowsocks use Xray's flat outbound settings (`address`, `port`, `id` / `password`, …) and `streamSettings` for the network and security. TCP is Xray's `raw`; its HTTP disguise names the hosts and paths, with a browser's usual headers (naming headers replaces Xray's defaults).
- mKCP's disguise and seed moved into Xray's `finalmask`: the header mask first (`header-wechat`, `header-srtp`, …), then `mkcp-aes128gcm` with the seed, or `mkcp-original` without one, which is how mKCP always obfuscated its packets. A `finalMask` the user gives replaces them.
- Hysteria2 is Xray's `hysteria` outbound over its `hysteria` transport: TLS with ALPN `h3` unless the link says otherwise, `salamander` as a UDP mask for the obfuscation password, and a QUIC keep-alive every 10 seconds (registered exception: QUIC closes a connection silent for 30 seconds, and NAT forgets idle UDP mappings).
- Xray no longer skips certificate verification (`allowInsecure` was removed on 2026-06-01). A self-signed certificate is trusted by its SHA-256 hash (`pinnedPeerCertSha256`); links that ask to skip verification import with a note.
- A custom outbound is passed on as written (numbers kept exactly), without its `tag`: the engine gives every outbound a tag of its own.
- No proxy reaches Xray without passing validation (`Outbound` validates first), whatever stored it: Xray accepts some values and then crashes on them while connecting, in a goroutine of its own where nothing can recover (a negative fragment length slices a packet backwards). `model/masks.go` keeps them out: no negative number anywhere in `finalMask`, XHTTP's `extra` or a custom outbound's masks and XHTTP settings (except `hKeepAlivePeriod`, where a negative means off), and masks from share links and the editor limited to the ones a server needs (the `header-*` disguises, `mkcp-original`, `mkcp-aes128gcm`, `salamander` with at least 4 bytes of password), none of which has a size to get wrong.
- `Engine.Check` builds the outbound and adds it to the instance under a tag of its own, then removes it; nothing connects. That finds what Xray checks only when it creates the handler (a VLESS Encryption key that is no key). `ProxyService.Create` / `Update` / `DraftLatency` call it (`Deps.CheckProxy`) after the model's validation, so Xray's objections (an unknown fingerprint, a removed transport in a custom outbound) show when saving, as `proxy.config` with Xray's words. A panic while building or adding an outbound becomes `proxy.config` too.
- The log bridge passes Xray's notes that a protocol or transport is deprecated (VMess, Trojan, Shadowsocks, WebSocket, gRPC, …) on as information: they come with every outbound built, and the protocol is the server's choice.

`probe.Latency` measures one HTTP GET of the test URL through any route: connecting, TLS for https, up to the response headers. Any HTTP status counts and redirects are not followed. There is no timeout; it is cancellable.

Linking Xray adds about 23 MB to the executable (measured with production build flags: 9.9 MB → 32.8 MB for the app without its frontend assets). The complete M4 build is 35.6 MB.

## Services [M4]

`internal/api.Core` is what the services share: the data, the settings, the `session.Manager`, the notices and each profile's latest session log. The app builds it with the real parts (`Deps`); tests pass stand-ins for Credential Manager, mstsc's registry memory and mstsc itself.

- **Credentials** (`Options.Credentials`): before mstsc starts, the session writes the profile's user name as `UsernameHint` (`HKCU\Software\Microsoft\Terminal Server Client\Servers\<server>`) and, if the user gave a password for this connection only, stores it as a one-time credential. mstsc keys `Servers` by the address without the port (seen on a real machine [M5]: its own entries, `CertHash` included, have no port although it connected to `127.x.y.z:13389`), so the hint goes there only; deleting the profile removes the address with any port, which earlier builds also wrote.
- **One lock for the vault**: a session starting, a session ending and a profile being edited can touch the same credential at once, so every vault operation goes through one lock, and look-then-change steps (store a one-time password unless one is remembered, move a password to a new user name, delete only the one-time password) happen inside it. A one-time password never replaces a password the app remembers.
- **Passwords and profile changes**:
  - a new target (host or port) deletes every saved password of the profile, because they belong to the old computer;
  - turning "remember password" off deletes the app's remembered password (mstsc's own stays);
  - a new user name moves the app's remembered password to it;
  - deleting a profile deletes its passwords and what mstsc remembers about its address;
  - a password given to `Connect` is saved as the remembered one when the profile remembers passwords, and used once otherwise.
- **Connected means locked**: while a profile's session has not ended (including one `Connect` has just started), neither the profile nor its proxy changes. `ProfileService.Update`, `SetProxy`, `ForgetPassword` and `Delete` return `session.running`; `ProxyService.Update` and `Delete` return `proxy.connected`, naming the connected profiles. A session goes on with what it started with, so a change would not reach it while the list already showed it (user's decision). These calls and `Connect` hold one lock (`Core.lifecycle`) from the check to the change, and `Update` holds it through its password clean-up, which would otherwise delete the one-time password of a session starting meanwhile. The engine still keeps each session's outbound by proxy ID and settings digest (M3), so nothing depends on this for correctness. The UI shows the editors read-only meanwhile.
- **The proxy of a profile** is chosen with `SetProxy` alone: `Update` keeps the stored one, so an editor without the field cannot set it back. A proxy has to exist when it is chosen (`Create`, `SetProxy`); a profile keeps one that is gone (its file could not be loaded), and the list marks it missing.
- **Deleting a proxy** switches the profiles that use it to direct, but only those in `moveToDirect`: the ones the UI listed and the user agreed to. Any other user leaves everything as it was, with `proxy.inUse` naming it, so the user is asked again. Profiles are rewritten before the proxy's file goes, so a failure midway never leaves a profile pointing at nothing. The log records which profiles switched. (Until 2026-10-03 a proxy in use could not be deleted at all.)
- **Default.rdp in preflight** (`mstsc.ReadDefaults` as `Deps.Defaults`) [M4, M7]: mstsc started with `/v:` takes everything but the address and the display switches from `Default.rdp` in the Documents known folder, so preflight reads it, together with the user's RD Gateway Group Policy (`HKCU\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`: `UseProxy`, `AllowExplicitUseProxy`, `ProxyName`). Settings that cannot be read add `session.defaultsUnknown` and do not stop the session; the error says why without Default.rdp's path, which goes into the log file and could name a file server or a company (a redirected Documents folder). The RD Gateway [M4]:
  - `gatewayprofileusagemethod` 0 ("Automatically detect RD Gateway server settings") means the administrator's settings apply: the file's own `gatewayusagemethod` and host are leftovers of the greyed-out explicit settings, often "always", and are ignored. Without an enabled policy there is then no gateway. A file without `gatewayprofileusagemethod` (hand-written) leaves open whether its usage applies, so "always" is only a warning there.
  - With explicit settings (`gatewayprofileusagemethod` 1), `gatewayusagemethod` 1 ("always") stops the session with `gateway.used`.
  - Values 2 and 3, or an enabled policy that is enforced or that `Default.rdp` defers to (`gatewayprofileusagemethod` 0), only add a warning to the session log: the policy uses the gateway when a direct connection fails, and mstsc always reaches the local tunnel directly.
  - [M7] "If server authentication fails: Do not connect" (`authentication level` 1) adds a warning (`session.serverAuthRefuse`): through the tunnel the certificate's name never matches 127.x.y.z, and Kerberos knows no such computer, so mstsc refuses unless the user trusted that certificate earlier ("Don't ask me again" stores the trust per address). That earlier trust is why it does not stop the session. The computer policy "Configure server authentication for client" (`HKLM\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`, `AuthenticationLevel` 0, 1 or 2), when enabled, decides instead of Default.rdp (`session.serverAuthRefusePolicy`: only an administrator changes it). Without Default.rdp, mstsc warns, which is also what it writes into a new one.
  - [M7] "Always ask for credentials" (`prompt for credentials` 1) adds a warning (`session.alwaysPrompt`) when a password is saved or given for the connection: mstsc asks all the same.
- **.rdp import** [M7]: `ProfileService.ParseRDP` takes the file's name and bytes (the frontend reads the file the user picks; files above `rdpfile.MaxSize`, 1 MiB, are refused on both sides) and returns a draft for the editor (`RDPImportView`): the computer, the user name with its domain, the display and `/admin`, named after the file; whether the file goes through an RD Gateway (the import connects through the proxy instead); the profiles that already connect to the same computer (host compared without case, port too). Nothing is stored until the editor's Create, and the proxy is chosen in the list as for any new profile. A file's redirection settings have no place in a profile (see Principles).
- **Diagnostics** [M7] (`internal/diag`, `DiagService`): `Report` gathers anew on every call (`Deps.Diagnose`): the versions (app, Xray, Windows with its UBR, mstsc.exe and mstscax.dll, WebView2), Default.rdp (found or not, RD Gateway, server authentication, always prompt), domain membership, the saved-credential delegation verdict, Credential Guard, CredSSP Encryption Oracle Remediation and the log folder. Each item is a key, a status (ok, info, warn, error), a value the UI translates (`diag.items.<key>.<value>`) or plain text, and maybe a detail (`diag.items.<key>.detail`); `diag.Codes` lists the keys for the translation test. Paths show the profile folder as `%USERPROFILE%`. Details that name the user's network or folders (the gateway, the servers a policy lists, Default.rdp's folder) are marked private and left out of copies. `EditDefaults` starts `mstsc /edit "<Documents>\Default.rdp"` (plain mstsc when there is none; saving there creates it) without tracking it; `OpenLogs` opens the log folder (ShellExecute, with COM set up as Microsoft asks).
  - Saved passwords through the tunnel: the server is authenticated with NTLM only, so "Allow delegating saved credentials with NTLM-only server authentication" decides. Not configured, a computer in no domain allows `TERMSRV/*` and one in a domain allows nothing; enabled, its list has to cover every entrance (`TERMSRV/*`, `*/*`, `*`; a name allows one wildcard, so one naming hosts cannot), or add the OS defaults on a computer in no domain; disabled allows nothing. "Deny delegating saved credentials" covering every server wins over both. When the OS defaults decide and the domain membership cannot be read, the verdict is "unknown".
  - Credential Guard is read from WMI (`Win32_DeviceGuard.SecurityServicesRunning` includes 1), with COM set up and undone per call on a locked thread. The isolated LSA process alone does not tell: Key Guard runs it too (seen on Windows 10 Pro, where Credential Guard is not available). WMI may take long or not answer at all (a damaged repository is just when people look at diagnostics), so the report never waits for it: the first report starts the question in the background (`Deps.CredentialGuard`, once per run, since Credential Guard changes only with a restart) and says "checking"; the answer is kept and `diag:changed` carries the report again. There is no time limit.
- **Notices**: problems found while loading, and failures of clean-up the user did not ask about (a password that could not be saved or deleted), become notices. They stay until dismissed; the frontend reads `AppService.Notices` at start and then listens to `app:notice`.
- **Startup**: the settings are first only read (`SettingsStore.Peek`, for the UI language and log level). Then the Wails application is created, which settles single-instance (a second launch hands over to the first and exits there). Only then does the app load the settings for real (moving an unreadable file aside), open the log file, load the data, start the engine, remove one-time passwords a crash may have left, and register the services. `RDP_OVER_PROXY_HOME` also makes the single-instance ID specific to that folder, so development runs and the user's app do not interfere.
- **Shutdown** (Wails `OnShutdown`): `Core.Quit` (every session gives everything back), then `Engine.Close`, then the log is closed.
- **Quitting** [M5] ends every remote desktop, so it asks first while any session has not ended. The window's Quit calls `AppService.Quit(false)`, which quits when nothing is connected and otherwise returns the count; the window asks and calls `Quit(true)`. The tray's Quit quits at once when nothing is connected; otherwise it brings the window forward and sends `app:quitRequested` (`Core.AskToQuit`), and the window asks the same question; cancelling calls `KeepRunning`. Choosing the tray's Quit again while that question is unanswered quits, in case the page cannot show it. `Quit` returns before the shutdown starts (`shell.quitLater`): the shutdown runs on the main thread and, in the server build, waits for the HTTP calls in flight, the asking call included.

## Errors [M4]

`errcode.WithArgs` attaches the arguments of an error's translated message (the RD Gateway's name, the connections using a proxy); they reach the UI in `ErrorView.args` and, in session log lines, as `errorArgs`.

`internal/errcode` gives errors stable dotted codes (`proxy.auth`, `probe.notRdp`, `net.refused`, …). Packages create their sentinel errors with `errcode.New` or `errcode.Weak`; `errors.Is` keeps working. `errcode.Of` picks the most useful code in an error tree: the first strong code, else one recognised from a Winsock error, else the first weak code ("the connection closed before the target answered" is weak, so a more specific cause wins), else `unknown`.

Xray reports why an outbound failed only as message text (its retry helper formats the errors it collected), so the engine labels them from the text, and the engine tests pin the texts with real proxies: `proxy.auth` (SOCKS5 account rejected, HTTP 407, Hysteria2 password refused [M6]), `proxy.unreachable` (the proxy server could not be reached), `proxy.targetFailed` (the proxy answered that it could not reach the target), `proxy.dropped` (the proxy accepted, then closed before the target answered, which is what Xray-based servers such as v2rayN's local port do when the target is unreachable), `proxy.tls` [M6] (the certificate is not the pinned one or does not verify; a REALITY server without the key shows its target's certificate). VMess, VLESS and Trojan servers say nothing to a client they do not know, so a wrong user ID or password only ends the connection (`probe.noAnswer`, whose text mentions it).

## Share links [M6]

`internal/sharelink` reads and writes links itself (libXray rejects v2rayN's VMess links and is tied to Xray's pre-releases):
- `vless://`, `trojan://` and the standard `vmess://uuid@host:port?…` follow XTLS/Xray-core discussion #716 (`type`, `security`, `sni`, `fp`, `pbk`, `sid`, `spx`, `pqv`, `pcs`, `vcn`, `ech`, `fm`, `extra`, …); Trojan is TLS unless the link says `security=none`, and the old `peer` stands for `sni`.
- v2rayN's `vmess://` is base64 of a JSON object whose members may be strings or numbers; `type`, `host` and `path` mean different things per network (gRPC: mode, authority, service name; mKCP: header type and seed). Version 1 kept the WebSocket path in the host (`host;path`).
- `ss://` is SIP002 (user info in base64, or percent-encoded for the 2022 methods) or the older all-base64 form; SIP003 plugins are refused (`link.unsupported`), since Xray cannot run them.
- `hysteria2://` / `hy2://` default to port 443; for port hopping the first port is used, with a note.
- `socks://`, `socks5://` and `http://` take the account plain or in base64, as v2rayN writes it.
- Query values are unescaped without turning `+` into a space (keys are base64), and the name comes from the fragment, cut off before anything else.
- `Parse` returns a normalized proxy, not a validated one: what is missing or wrong shows in the editor next to its field. It refuses only what cannot become a proxy at all: an invalid link, an unknown scheme, HTTP/2 or QUIC (`link.transportRemoved`), the old XTLS, an unknown Hysteria2 obfuscation.
- Notes (`linkNotes.<code>`): a request to skip certificate verification (TLS only), a VMess alterId above 0 (Xray speaks only VMess AEAD), parameters that were left out, port hopping.
- `Format` writes VMess in v2rayN's format unless the proxy uses what that format cannot hold (REALITY, pinned certificates, `fm`, …), then in the standard one; the others in their own schemes. v2rayN's format carries the name inside (`ps`) and gets no `#name`: its readers decode everything after `vmess://` as base64. A proxy comes back the same after `Format` and `Parse` (tested for each kind, including Hysteria2's `vcn`). A custom outbound has no link (`link.notShareable`).
- mKCP without a disguise, a seed or `fm` gets `mkcp-original`, the obfuscation every mKCP server used before Xray 26; a current Xray server without masks speaks bare mKCP, which `fm={"udp":[]}` reaches.

## Logging [M4]

- `%LOCALAPPDATA%\RDP-over-proxy\logs\app.log`, rotated by size: at most 2 MiB each, two older files kept.
- The file is meant to be attachable to a bug report. Before a line is written, the user's profile folder becomes `%USERPROFILE%` (paths name the Windows account); the hosts, servers, RD Gateway, user names, connection, group and proxy names and proxy passwords the app knows become `<redacted>`, and so do the V2Ray family's user IDs, SNI, hosts, paths, service names, keys and seeds, the passwords in masks and XHTTP settings, and the addresses, IDs, passwords, keys, server names and header values in a custom outbound [M6]; and every IP address except loopback and unspecified ones becomes `<ip>`. Words that name nothing (a service called "grpc", a path "/ws", a local SOCKS port's 127.0.0.1) are not masked: they would garble every line that mentions them. Names only ever join the set: a running session may still use a host the data no longer has. The in-memory rings (the app's and each session's) keep the details for the user's own screen.
- Consecutive identical lines are written once; when a different line follows, a note says how often the previous one repeated.
- The level comes from the settings and changes at once. Xray's info and debug lines are forwarded only at debug level.
- Only the Wails runtime's warnings and errors are kept, at every level: some of its debug records carry the arguments of service calls, passwords included, and it logs every asset it serves. Its reports of errors that service methods returned are left out too (the UI gets them), and attributes that carry payloads (`args`, `result`, …) are written as `<omitted>`.

## Tests [M5]

Tests live apart from the code: the Go tests in `tests/<package>/`, one folder per package under test (`tests/session` tests `internal/session`), and the frontend tests in `frontend/tests/`, mirroring `frontend/src`. Nothing under `internal/` or `frontend/src` is test code.

- A Go test folder holds an external test package (`package session_test`) and uses only what the package exports. Tests that used to sit inside their package dot-import it (`import . "…/internal/session"`), the use the Go FAQ gives for dot imports, so their bodies read as before.
- Packages are tested through their public API: dependencies come in as interfaces (`api.Deps`, `session.Options`, `session.Process`), so tests pass stand-ins. Lists that exist for completeness checks are exported in the same spirit as the code lists (`session.Messages`, `api.NoticeCodes`, `errcode.All`, `i18n.Keys` with `i18n.Lookup`). Nothing is exported only for a test.
- `tests/testutil` (fake RDP server, helper processes, `FreePort`, `FreeUDPPort`) and `tests/testutil/xraytest` (in-process Xray proxies) are the shared test support; `tests/rdpfile/testdata` holds sample `.rdp` files.
- `xraytest` [M6] starts a server for the client settings a test gives (`Options`): the protocol's inbound, the same network written the way a server's configuration says it, a self-signed certificate for TLS and Hysteria2 (`Model` pins its hash), and for REALITY a key and a local TLS 1.3 site to borrow handshakes from. The site ends each connection after the handshake: a REALITY server first reads what its target sends after a handshake until it closes, or 5 seconds. The servers get a 4-second handshake time; Xray's default of 60 is how long a VMess server holds a client it does not know.
- `go test ./...` runs everything; the CI does the same after building the frontend.

## Frontend structure

- `src/app` — shell with sidebar (Quit at the bottom), theme (follows Windows via `prefers-color-scheme`), first-run language picker [M0]; notices bar and quit confirmation [M5]
- `src/features/connections` [M5] — the list by group with each session's state and buttons (Connect / Cancel / Show window + Disconnect / End now); the profile editor; the password prompt; the route check; the session log drawer. `status.ts` maps a `SessionView` to the row's colour, text and buttons (`isActive`: the session has not ended); `profileForm.ts` converts between the form and `model.Profile` (one address field takes `host`, `host:port`, `[IPv6]:port`). A row that is not connected chooses its proxy in a dropdown (`SetProxy`); a connected row says "via …" instead, which leaves room for Show window. The editor has no proxy field, and while connected it is read-only, as is "Forget saved password". [M7]: "Import .rdp" (a hidden file input, `rdpImport.ts`) opens the editor of a new profile filled in from the file, with notes on what was read, an RD Gateway and existing connections to the same computer; the editor's display hint links to "Change Remote Desktop's default settings". The route check (`CheckDialog`, `routeTest.ts`) runs `ProxyService.Latency` (the proxy alone, fetching the test URL; skipped when direct) and `SessionService.CheckRoute` (the computer through the proxy) at once, shows each step and the security the computer asks for, and concludes where the problem is: beyond the proxy when the proxy alone works, the proxy when both fail. A display-only counter shows the seconds; closing cancels both, "Test again" starts over.
- `src/features/diagnostics` [M7] — the report by group with a status icon per item; Check again, Copy diagnostics (`report.ts`: plain text without private details, `[!]` and `[x]` marking what gets in the way), Change Remote Desktop's default settings (`useEditDefaults`, also used by the profile editor), Open the log folder.
- `src/features/proxies` [M5] — the list (built-in Direct first) with latency tests that can be cancelled; the SOCKS5 / HTTP editor (`proxyForm.ts`; an empty password field keeps the saved one). [M6]: the editor covers every kind: a share-link field fills the form (`ParseLink`) and shows the link's notes; per kind the account, the secret, the protocol's options, the transport and the security layer (`sections`); settings few people change fold away; a custom kind takes outbound JSON; "Test" measures the unsaved settings (`DraftLatency`). Choosing another kind starts its settings over from that kind's defaults, and a stored secret is kept only for the same kind (SOCKS5 and HTTP accounts count as one); another network starts the mode and disguise over. A test still running when the settings change is cancelled. A proxy whose secrets were lost shows a warning, in the list and in its editor. The list shows the transport ("WebSocket + TLS"); the menu copies a proxy's share link. The editor of a saved proxy opens once the proxy has loaded: a Fluent dialog that opens with nothing to focus never starts its focus trap, and then sends focus out of the dialog whenever it lands inside. `usage.ts` finds a proxy's users and the connected ones: deleting asks first and names the connections that will switch to direct, or, while one is connected through the proxy, says so and keeps Delete disabled; the editor is read-only meanwhile (Test still works).
- `src/features/settings` — appearance, close behaviour, about [M0]; local port, check-first, test URL, log level [M5]
- `src/components` — `Page`, `EmptyState` [M0]; `Feedback`: toasts, `ErrorBar`, `ConfirmDialog` [M5]
- `src/stores` — zustand stores fed by service calls and Go events: `settings` [M0]; `data` (profiles, proxies, sessions, session logs, notices, quit confirmation) [M5]. Replies and events travel separately: what an event changed while the first read was in flight is kept over the reply; session log lines carry a sequence number (`logging.Line.Seq`, given by `Core`) so a log read and the lines sent as events merge without duplicates; settings saves run one after another, each on top of the last stored settings.
- `src/lib` [M5] — pure helpers with tests: address splitting, translating error / notice / log codes (`messages.ts`), session log merging (`sessionLog.ts`); base64 for `[]byte` arguments, the clipboard [M7]
- `src/locales` — `zh-CN.json` and `en.json`; a test enforces identical keys [M0]; another checks the keys the frontend builds from codes (phases, steps, outcomes, field errors) [M5], and proxy kinds, networks and security layers [M6]; the Go side checks `linkNotes.<code>` [M6]
- `tests/` — the vitest tests, mirroring `src/` (`tests/lib/address.test.ts` tests `src/lib/address.ts`) [M5]

**Conventions** [M5]
- Forms send what the user typed; the Go side validates and returns field paths, which each form maps to its fields (`formField`). The forms set `noValidate`, so the browser's required-field bubbles do not pre-empt those messages. The frontend only reports what the Go side cannot see, such as a port that is not a number.
- Errors are shown as the translation of `errors.<code>`. The original English text is added as details only for codes where it can help (network, proxy, probe, tunnel, Credential Manager, DPAPI, unknown).
- Before connecting, the password prompt appears only when the profile has a user name and neither the app nor mstsc has a password for it. It can change "remember" (saved to the profile first) or be skipped so that mstsc asks.

## Build

`wails3 build` runs `build/Taskfile.yml` and `build/windows/Taskfile.yml`: install frontend deps, generate bindings, `vite build`, generate the Windows resource (`.syso` from `build/windows/icon.ico`, `info.json` and `wails.exe.manifest`), then `go build -tags production -trimpath -ldflags "-w -s -H windowsgui"`. Only Windows build files are kept.

The build does not generate the icon. `build/icon` holds the drawings: `appicon.svg` for 96 px and up, and a pixel-aligned redrawing for each size from 16 to 64 px (`appicon-<size>.svg`) so the title bar, taskbar and tray icons stay sharp. `wails3 task common:generate:icons` (`go run build/icon/generate.go`) has headless Edge draw them and writes `build/appicon.png` (1024 px) and `build/windows/icon.ico` (16, 20, 24, 32, 40, 48, 64, 96 as bitmaps, 256 as PNG); both are checked in. `main` embeds the same `icon.ico` for the tray, where Wails picks the image matching the small icon size.
