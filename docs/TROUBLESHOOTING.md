# 常见问题排查 / Troubleshooting

[中文](#中文) | [English](#english)

## 中文

### 启动时提示「缺少 WebView2 运行时」

界面需要 Microsoft Edge WebView2 运行时才能显示。Windows 11 自带它，大多数 Windows 10 也已经随 Edge 装好。

如果没有，请从 [微软官网](https://developer.microsoft.com/microsoft-edge/webview2/) 下载 Evergreen Bootstrapper，安装后再启动本程序。

### 杀毒软件或 SmartScreen 报警

程序没有代码签名，而且内嵌了代理核心，所以部分安全软件会误报。

- 请只从本项目的 GitHub Releases 下载。
- 下载后用发布页提供的 SHA256 校验值核对文件。
- 发布版本由 GitHub Actions 从公开源码构建。

### 双击后没看到窗口

程序只允许运行一个实例，关闭主窗口后会缩到任务栏右下角的托盘里继续运行。有两种方法找回窗口：

- 单击托盘图标。
- 再双击一次程序，已经在运行的窗口会被切到前台。

如果希望关闭窗口时直接退出，在「设置 → 关闭主窗口时」里选择「退出程序」。

### 想把设置恢复成默认值

先退出程序（托盘图标 → 退出），然后删除 `%APPDATA%\RDP-over-proxy\settings.json`。

如果设置文件损坏，程序会自动把它改名为 `settings.json.corrupt` 留作备份，然后使用默认设置启动。

### 远程桌面窗口标题显示 127.x.y.z

这是正常的。mstsc 实际连接的是本机的隧道入口，所以它只知道这个地址。每个连接的回环地址是固定的，mstsc 记住的密码和证书信任不会在不同电脑之间混用。

### 日志文件在哪里

在 `%LOCALAPPDATA%\RDP-over-proxy\logs\app.log`（把这个路径粘贴到资源管理器的地址栏即可打开）。文件写满 2 MB 后改名为 `app.1.log`，最多保留两个旧文件。

写入日志前，程序会把你填写过的主机名、代理服务器地址、用户名、连接和代理的名称，以及代理的用户 ID、服务器名称（SNI）、路径、密钥等替换成 `<redacted>`，把用户目录（路径里有你的 Windows 账户名）替换成 `%USERPROFILE%`，把除本机回环地址以外的 IP 地址替换成 `<ip>`，方便附到 Issue 里。密码不会写进日志。附上之前仍请自己检查一遍。

### 换了电脑或 Windows 用户后，代理的密码没了

代理的密码（以及 VMess、VLESS 的用户 ID 和传输、TLS 等设置）在文件里是用 Windows 的 DPAPI 按当前用户加密的，只有同一个 Windows 用户才能解开。把 `%APPDATA%\RDP-over-proxy` 复制到别的电脑或别的用户下，代理和连接都还在，但这些内容需要重新填写（重新粘贴分享链接最快）。代理页会在这类代理旁标出「需要重新填写」，重新保存之前不会用它连接：留下的只是默认设置，照着默认设置连接的话，用户 ID 可能不经加密就发出去。程序启动时会记下是哪些文件（写在日志里，界面完成后也会在窗口里提示）。

如果某个文件损坏、无法读取，程序会把它改名为 `.corrupt` 保留下来，其余的照常载入。

### 粘贴分享链接后提示「Xray 已经不支持 h2 / quic 传输」

Xray 已经删掉了 HTTP/2（h2）和 QUIC 两种传输方式，这类节点用 Xray 连不上。请服务提供者换成 XHTTP、WebSocket、gRPC 等其他传输方式。

### 提示「链接要求跳过证书验证」，或连接时提示证书没有通过验证

Xray 从 2026 年 6 月起不再允许跳过证书验证（`allowInsecure`、`insecure=1`）。程序导入这类链接时会照常验证证书：

- 服务器用的是正规证书：不用管，能正常连接。
- 服务器用的是自签名证书：在代理的「证书指纹（SHA-256）」里填这张证书的 SHA-256，程序就只认这一张证书。指纹可以向服务提供者要；Hysteria2 的链接里常带着 `pinSHA256`，导入时会自动填好。

用 REALITY 时出现这个提示，一般是公钥（pbk）或 Short ID 不对。

### VMess、VLESS、Trojan 代理连不上，只提示「远程计算机应答之前，连接就被关闭了」

这几种协议的服务器不会告诉客户端哪里不对：用户 ID 或密码错了，它只是把连接关掉（VMess 服务器还会拖一阵才关）。请先核对用户 ID / 密码，再用代理页的「测速」确认代理本身能用。

VMess 链接里的 alterId 不是 0 时，导入会提示它被忽略了：Xray 只支持 VMess AEAD。服务器还要求旧的验证方式的话，需要服务端改设置。

---

## English

### "WebView2 Runtime is missing" at startup

The window needs the Microsoft Edge WebView2 Runtime. Windows 11 includes it, and most Windows 10 PCs already have it through Edge.

If yours doesn't, download the Evergreen Bootstrapper from [Microsoft](https://developer.microsoft.com/microsoft-edge/webview2/), install it, then start the app again.

### Antivirus or SmartScreen warnings

The program isn't code-signed and embeds a proxy core, so some security products flag it by mistake.

- Download it only from this project's GitHub Releases.
- Check the file against the SHA256 checksums published with each release.
- Releases are built by GitHub Actions from the public source.

### Nothing appears when I start it

The app runs as a single instance and minimizes to the notification area (tray) when you close its window. To bring the window back, either:

- click the tray icon, or
- start the app again; the window that's already running comes to the front.

To quit when the window is closed instead, choose "Quit the app" under Settings → When the main window is closed.

### Resetting the settings

Quit the app (tray icon → Quit), then delete `%APPDATA%\RDP-over-proxy\settings.json`.

If the settings file is ever unreadable, the app renames it to `settings.json.corrupt` as a backup and starts with the defaults.

### The Remote Desktop title bar shows 127.x.y.z

That's expected. mstsc connects to the local tunnel entrance, so that's the only address it knows. Each connection's loopback address never changes, so the passwords and certificate trust mstsc remembers stay separate for each computer.

### Where's the log file?

It's `%LOCALAPPDATA%\RDP-over-proxy\logs\app.log` (paste that path into File Explorer's address bar). When it reaches 2 MB it's renamed to `app.1.log`; at most two older files are kept.

Before writing a line, the app replaces the host names, proxy server addresses, user names and connection and proxy names you entered, and proxies' user IDs, server names (SNI), paths and keys, with `<redacted>`, your user folder (its path contains your Windows account name) with `%USERPROFILE%`, and every IP address other than this computer's loopback addresses with `<ip>`, so the log can be attached to an issue. Passwords never go into the log. Please still look it over before you attach it.

### Proxy passwords are gone after moving to another PC or Windows user

Proxy passwords (and the user IDs and transport and TLS settings of VMess and VLESS proxies) are encrypted in their files with Windows DPAPI for the current user, and only the same Windows user can decrypt them. If you copy `%APPDATA%\RDP-over-proxy` to another PC or user, the proxies and connections are all there, but these have to be entered again (pasting the share links again is quickest). The Proxies page marks such proxies "Needs re-entering", and they aren't used until saved again: what's left are default settings, and connecting with them could send a user ID without the encryption it was meant to travel in. The app notes which files are affected when it starts (in the log, and in the window once the connection pages are done).

If a file is damaged and can't be read, the app renames it to `.corrupt` and keeps it; everything else loads as usual.

### Pasting a share link says Xray no longer has the h2 / quic transport

Xray has removed the HTTP/2 (h2) and QUIC transports, so such servers can't be reached with Xray. Ask the server's provider to switch to XHTTP, WebSocket, gRPC or another transport.

### "The link asks to skip certificate checks", or the certificate didn't pass when connecting

Since June 2026 Xray no longer skips certificate verification (`allowInsecure`, `insecure=1`). The app imports such links and checks the certificate as usual:

- A server with a proper certificate connects as before.
- For a self-signed certificate, enter its SHA-256 hash under the proxy's "Certificate fingerprint (SHA-256)"; the app then trusts exactly that certificate. The provider can give you the hash; Hysteria2 links often carry it as `pinSHA256`, which is filled in on import.

With REALITY, this usually means the public key (pbk) or short ID is wrong.

### A VMess, VLESS or Trojan proxy only says "The connection closed before the remote computer answered"

Servers of these protocols don't tell a client what's wrong: with a wrong user ID or password they just close the connection (a VMess server waits a while first). Check the user ID or password, then use Test on the Proxies page to make sure the proxy itself works.

If a VMess link has an alterId other than 0, the import notes that it was ignored: Xray speaks only VMess AEAD. A server that still requires the old authentication needs its settings changed.
