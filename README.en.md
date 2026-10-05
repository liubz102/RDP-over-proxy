# RDP over Proxy

[中文](README.md) | [English](README.en.md)

Run Windows' built-in Remote Desktop (mstsc) through a proxy: SOCKS5 and HTTP, plus VMess, VLESS, Trojan, Shadowsocks and Hysteria2.

> **In development:** this is the 0.1.0 preview. It already connects Remote Desktop through SOCKS5, HTTP and V2Ray-family proxies, imports `.rdp` files and shows environment diagnostics; the installer is still being built. See [docs/PROGRESS.md](docs/PROGRESS.md) for progress.

## Why

Remote Desktop gets sluggish across regions or over a poor route. A proxy server with a good route can make a big difference, but mstsc has no proxy support at all — only Microsoft's RD Gateway. The usual workarounds:

- Hand-writing a v2ray config to forward a port.
- Running TUN mode or Proxifier, which needs administrator rights and takes over all of the PC's traffic.

RDP over Proxy does exactly this one job:

- **Native mstsc.** You keep its image quality, multi-monitor support, clipboard and everything else.
- **Saved connections.** Settings are kept per target computer. Proxies are managed separately and can be shared by many connections.
- **Built-in proxy protocols.** Import a proxy by pasting a share link.
- **Nothing system-wide.** No administrator rights, no drivers and no system routing changes; only Remote Desktop is affected.

## How it works

```
mstsc ──▶ 127.x.y.z:13389 ──▶ RDP over Proxy (local tunnel) ──▶ proxy server ──▶ target PC
```

Each connection gets a fixed loopback address `127.x.y.z` on your PC. mstsc connects to that address, and the tunnel carries the traffic through the proxy you chose. The proxy protocols come from the embedded [Xray-core](https://github.com/XTLS/Xray-core); you don't need to run v2ray or Xray separately.

## Features

Done (✓) and planned:

- ✓ Chinese and English UI; pick a language on first launch, change it later in Settings
- ✓ Minimizes to the tray on close; runs as a single instance; follows the system light/dark theme
- ✓ Connections: target address and port, the proxy to use, the sign-in user name, and an optional saved password (kept in Windows Credential Manager)
- ✓ Proxies: SOCKS5, HTTP, VMess, VLESS (including REALITY), Trojan, Shadowsocks, Hysteria2 and custom Xray outbounds
- ✓ Import by pasting share links, including links exported by v2rayN; copy a proxy's share link
- ✓ Route check before connecting: sends an RDP handshake to the target through the proxy and reports the latency and the security protocols the target accepts
- ✓ Step-by-step route check: tests the proxy on its own and the remote computer through it, and says which part fails
- ✓ One-click connect; the tunnel closes when the Remote Desktop window does
- ✓ Import connections from `.rdp` files
- ✓ Diagnostics page: the versions of Windows and Remote Desktop, the default Remote Desktop settings that affect connections, whether saved passwords can be used; copy the diagnostics without server names
- ✓ Everything the app keeps is in its own folder; nothing goes into your user profile
- Installer

## Requirements

- Windows 10 or 11 (64-bit)
- Microsoft Edge WebView2 Runtime. Windows 11 includes it; most Windows 10 PCs already have it through Edge.

## Where the data is

All of it is in the folder the app runs from, in plain sight:

```
RDP-over-proxy\
├─ RDP-over-proxy.exe
├─ data\      settings, proxies, connections, and the window's cache (WebView2)
└─ logs\      the log
```

Put the app in a folder you can write to (such as `D:\Tools\RDP-over-proxy`). In a place that needs administrator rights, such as `C:\Program Files`, the app explains on its first start and asks for administrator rights once, only to create those two folders.

Proxy passwords are encrypted with Windows in these files, so only the same Windows user on the same PC can read them. Remote Desktop passwords are kept in Windows Credential Manager, because Remote Desktop Connection reads them only from there.

## Building from source

You need Go 1.27, Node.js 24 and the Wails v3 CLI:

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27
wails3 build      # produces bin\RDP-over-proxy.exe
wails3 dev        # development mode with hot reload
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for more.

## FAQ

**Why does my antivirus or SmartScreen warn about it?**
The program isn't code-signed and embeds a proxy core, which some security products flag by mistake. Every release is built by GitHub Actions from the public source and comes with SHA256 checksums.

**Why is there a 127.x.y.z in the Remote Desktop title bar?**
The title starts with the connection's name; the 127.x.y.z after it is the local tunnel entrance mstsc actually connects to: mstsc knows nothing of the proxy, only this address. Each connection's loopback address never changes, so the passwords and certificate trust mstsc remembers stay separate for each computer.

More in [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md).

## License

This project is released under the [MIT License](LICENSE).

Proxy protocols are provided by [Xray-core](https://github.com/XTLS/Xray-core) (MPL-2.0), used unmodified as a library. Third-party license notices ship with each release.

## Acknowledgements

[Xray-core](https://github.com/XTLS/Xray-core) · [Wails](https://wails.io) · [Fluent UI](https://react.fluentui.dev)
