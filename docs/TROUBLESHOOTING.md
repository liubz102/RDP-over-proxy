# 常见问题排查 / Troubleshooting

[中文](#中文) | [English](#english)

## 中文

### 启动时提示「缺少 WebView2 运行时」

界面需要 Microsoft Edge WebView2 运行时才能显示。Windows 11 自带它，大多数 Windows 10 也已经随 Edge 装好。

如果没有，请从 [微软官网](https://developer.microsoft.com/microsoft-edge/webview2/) 下载 Evergreen Bootstrapper，安装后再启动本程序。

### 数据保存在哪里

全部在程序所在的文件夹里，不往 `%APPDATA%` 等用户目录写任何东西：

```
RDP-over-proxy\
├─ RDP-over-proxy.exe
├─ data\      settings.json（设置）、proxies\（代理，每个一个文件）、profiles\（连接，每个一个文件）、WebView2\（界面的缓存）
└─ logs\      app.log（日志）
```

「设置」页的「数据」一节显示这两个文件夹，可以直接打开。远程桌面的密码不在这里：「远程桌面连接」只从 Windows 凭据管理器读密码，所以密码存在那里。

### 启动时提示「需要管理员权限」

程序放在了只有管理员才能写入的地方（例如 `C:\Program Files`），建不了 `data` 和 `logs` 文件夹。点「确定」后 Windows 会请求一次管理员权限，只用来建这两个文件夹并允许你的账户写入；程序本身仍以普通权限运行，以后启动不会再问。

不想给管理员权限的话，点「取消」，把整个程序文件夹移到别处（例如 `D:\Tools`）再启动。

### 启动时提示「无法保存数据」

程序所在的文件夹不能写入，例如在只读的盘、光盘或只读的网络共享上，或者你拒绝了管理员权限。提示里会写出是哪个文件夹、Windows 给的原因。把整个程序文件夹移到可以写入的地方再启动。

### 杀毒软件或 SmartScreen 报警

程序没有代码签名，而且内嵌了代理核心，所以部分安全软件会误报。

- 请只从本项目的 GitHub Releases 下载。
- 下载后用发布页提供的 SHA256 校验值核对文件。
- 发布版本由 GitHub Actions 从公开源码构建。

### 双击后没看到窗口

同一个文件夹里的程序只运行一个实例，关闭主窗口后会缩到任务栏右下角的托盘里继续运行。有两种方法找回窗口：

- 单击托盘图标。
- 再双击一次程序，已经在运行的窗口会被切到前台。

如果希望关闭窗口时直接退出，在「设置 → 关闭主窗口时」里选择「退出程序」。

### 想把设置恢复成默认值

先退出程序（托盘图标 → 退出），然后删除程序文件夹里的 `data\settings.json`。

如果设置文件损坏，程序会自动把它改名为 `settings.json.corrupt` 留作备份，然后使用默认设置启动。

### 远程桌面窗口标题显示 127.x.y.z

这是正常的。mstsc 实际连接的是本机的隧道入口，所以它只知道这个地址。每个连接的回环地址是固定的，mstsc 记住的密码和证书信任不会在不同电脑之间混用。

### 先看「诊断」页

侧栏的「诊断」页列出这台电脑上影响远程桌面连接的东西：Windows、远程桌面连接（mstsc）和 WebView2 的版本，远程桌面的默认设置（Default.rdp）和相关组策略里的 RD 网关、服务器身份验证、「始终要求凭据」，以及决定能不能用保存的密码的组策略。它只读取，不改任何设置。有问题的项目用黄色或红色标出，并写明在哪里改。

报告问题时，点「复制诊断信息」粘贴到 Issue 里：复制出来的文字不含服务器名称和文件夹路径。

### 剪贴板、驱动器、声音这些在哪里设置

本程序用 `mstsc /v:` 启动远程桌面（打开 .rdp 文件从 2026 年 4 月起每次都会弹安全提示），这种方式下每个连接只能单独设置显示（全屏、窗口大小、多显示器）和 `/admin`，其余设置对所有连接都一样，来自「文档」里的 `Default.rdp`。

要修改它们，在「诊断」页或连接的编辑框里点「调整远程桌面默认设置」：会打开「远程桌面连接」，改好「显示」「本地资源」「体验」「高级」各页后，回到「常规」页点「保存」。不点「保存」的话改动不会保留。

### 从 .rdp 文件导入连接

连接页的「从 .rdp 导入」会读出文件里的计算机地址和端口、用户名（连同 `domain`）、显示设置和管理会话，填进新建连接的表单，检查后保存即可；代理在列表里选。文件里的剪贴板、驱动器等设置不会导入（见上一条）。文件要求通过 RD 网关连接时会提示：导入后改为经代理直接连接那台计算机。已经有连到同一台计算机的连接时也会提示。

### 「检查线路」的结果怎么看

连接的「更多 → 检查线路」同时做两件事，不启动远程桌面：

- **代理**：经代理访问「设置」里的测速网址，确认代理本身能用。直连时跳过。
- **远程计算机**：经代理请求远程计算机上的远程桌面服务应答，应答后显示它要求的安全协议，例如 HYBRID_EX 表示网络级身份验证（NLA）。

两步合起来能分清问题在哪：代理正常、远程计算机不应答，问题在代理之后（计算机地址、端口、远程计算机没开机或没允许远程桌面、防火墙）；两步都失败，多半是代理本身的问题（服务器、端口、用户 ID 或密码）；远程计算机应答了但测速网址打不开，不影响远程桌面。检查没有时间限制，等多久由你决定，随时可以取消。

### 保存了密码，远程桌面还是每次都问

先看「诊断」页「保存的密码」一节：

- **经隧道使用保存的密码：不允许**。经隧道连接时 mstsc 连的是 `127.x.y.z`，这个名字没有 Kerberos 身份，只能用 NTLM 验证服务器；而加入域的电脑默认不允许这时使用保存的密码，远程桌面会提示「你的系统管理员不允许使用保存的凭据」。需要管理员在组策略「计算机配置 → 管理模板 → 系统 → 凭据分配 → 允许分配保存的凭据用于仅 NTLM 服务器身份验证」里启用并加入 `TERMSRV/*`。本程序不改组策略。
- **始终要求凭据：是**。远程桌面的默认设置勾选了「始终要求凭据」，保存的密码不会被自动填入。在「调整远程桌面默认设置」的「常规」页取消勾选并保存。这种情况下连接时会话日志里也有提示。
- **Credential Guard：正在运行**。远程桌面自己在登录框里「记住我」存的密码可能用不上；在本程序的连接里勾选「记住密码」保存的不受影响。

### 远程桌面提示证书问题，或者说无法验证远程计算机的身份

经隧道连接时，mstsc 连的地址是 `127.x.y.z`，和远程计算机证书上的名字永远对不上，所以第一次连接某台计算机时会提示证书问题。确认是你要连的电脑后，勾选「不再询问我是否连接到此计算机」再连接，之后就不再提示（信任按每个连接的回环地址记住）。

如果远程桌面直接拒绝连接，看「诊断」页的「如果服务器身份验证失败」：设成「不连接」的话，除非以前已经信任过那台计算机的证书，否则一律拒绝。在「调整远程桌面默认设置」的「高级」页改成「显示警告」并保存。如果是组策略「为客户端配置服务器身份验证」设的，只能由管理员修改。

### 提示「远程桌面被设置为总是通过 RD 网关连接」

RD 网关在远程桌面那一侧去连目标，连不到本机的隧道入口，所以经本程序的连接必须不走网关。在「调整远程桌面默认设置」→「高级」→「从任意位置连接」的「设置」里，选「不使用 RD 网关服务器」或「自动检测 RD 网关服务器设置」，回到「常规」页点「保存」。「诊断」页能看到当前的网关设置；如果是组策略启用了网关，只会在直连失败时才改走网关，本程序只在会话日志里提醒。

### 提示「Windows 不允许本程序使用这个本地端口」

Hyper-V、WSL、Docker 等会保留一些端口范围，程序不能在这些端口上监听。在「设置 → 连接 → 本机入口端口」换一个端口（例如 23389），对之后的连接生效。可以用 `netsh interface ipv4 show excludedportrange protocol=tcp` 查看被保留的范围。

### 日志文件在哪里

在程序文件夹里的 `logs\app.log`（「设置」页「数据」一节或「诊断」页都能打开日志文件夹）。文件写满 2 MB 后改名为 `app.1.log`，最多保留两个旧文件。

写入日志前，程序会把你填写过的主机名、代理服务器地址、用户名、连接和代理的名称，以及代理的用户 ID、服务器名称（SNI）、路径、密钥等替换成 `<redacted>`，把用户目录（路径里有你的 Windows 账户名）替换成 `%USERPROFILE%`，把除本机回环地址以外的 IP 地址替换成 `<ip>`，方便附到 Issue 里。密码不会写进日志。附上之前仍请自己检查一遍。

### 换了电脑或 Windows 用户后，代理的密码没了

代理的密码（以及 VMess、VLESS 的用户 ID 和传输、TLS 等设置）在文件里是用 Windows 的 DPAPI 按当前用户加密的，只有同一个 Windows 用户才能解开。把程序文件夹复制到别的电脑，或者换一个 Windows 用户运行，代理和连接都还在，但这些内容需要重新填写（重新粘贴分享链接最快）。代理页会在这类代理旁标出「需要重新填写」，重新保存之前不会用它连接：留下的只是默认设置，照着默认设置连接的话，用户 ID 可能不经加密就发出去。程序启动时会记下是哪些文件（写在日志里，界面完成后也会在窗口里提示）。

如果某个文件损坏、无法读取，程序会把它改名为 `.corrupt` 保留下来，其余的照常载入。

### 粘贴分享链接后提示「Xray 已经不支持 h2 / quic 传输」

Xray 已经删掉了 HTTP/2（h2）和 QUIC 两种传输方式，这类节点用 Xray 连不上。请服务提供者换成 XHTTP、WebSocket、gRPC 等其他传输方式。

### 提示「链接要求跳过证书验证」，或连接时提示证书没有通过验证

Xray 从 2026 年 6 月起不再允许跳过证书验证（`allowInsecure`、`insecure=1`）。程序导入这类链接时会照常验证证书：

- 服务器用的是正规证书：不用管，能正常连接。
- 服务器用的是自签名证书：在代理的「证书指纹（SHA-256）」里填这张证书的 SHA-256，程序就只认这一张证书。指纹可以向服务提供者要；Hysteria2 的链接里常带着 `pinSHA256`，导入时会自动填好。

用 REALITY 时出现这个提示，一般是公钥（pbk）或 Short ID 不对。

### VMess、VLESS、Trojan 代理连不上，只提示「远程计算机应答之前，连接就被关闭了」

这几种协议的服务器不会告诉客户端哪里不对：用户 ID 或密码错了，它只是把连接关掉（VMess 服务器还会拖一阵才关）。用连接的「检查线路」区分：「代理」一步也失败的，多半是用户 ID / 密码不对；「代理」通过、「远程计算机」失败的，问题在代理之后。

VMess 链接里的 alterId 不是 0 时，导入会提示它被忽略了：Xray 只支持 VMess AEAD。服务器还要求旧的验证方式的话，需要服务端改设置。

---

## English

### "WebView2 Runtime is missing" at startup

The window needs the Microsoft Edge WebView2 Runtime. Windows 11 includes it, and most Windows 10 PCs already have it through Edge.

If yours doesn't, download the Evergreen Bootstrapper from [Microsoft](https://developer.microsoft.com/microsoft-edge/webview2/), install it, then start the app again.

### Where is my data?

All of it is in the folder the app runs from; nothing goes into `%APPDATA%` or anywhere else in your user profile:

```
RDP-over-proxy\
├─ RDP-over-proxy.exe
├─ data\      settings.json (settings), proxies\ (one file per proxy), profiles\ (one file per connection), WebView2\ (the window's cache)
└─ logs\      app.log (the log)
```

The Data section of the Settings page shows both folders and opens them. Remote Desktop passwords aren't there: Remote Desktop Connection reads passwords only from Windows Credential Manager, so that's where they're kept.

### "Administrator rights needed" at startup

The app is in a folder only administrators can write to (such as `C:\Program Files`), so it can't create its `data` and `logs` folders. Choose OK and Windows asks for administrator rights once; they're used only to create those two folders and let your account write to them. The app itself keeps running without them and won't ask again.

If you'd rather not, choose Cancel, move the whole app folder somewhere else (such as `D:\Tools`), and start it again.

### "Can't save data" at startup

The app's folder can't be written to: it's on a read-only drive, a disc or a read-only network share, or administrator rights were declined. The message names the folder and the reason Windows gave. Move the whole app folder somewhere you can write to and start it again.

### Antivirus or SmartScreen warnings

The program isn't code-signed and embeds a proxy core, so some security products flag it by mistake.

- Download it only from this project's GitHub Releases.
- Check the file against the SHA256 checksums published with each release.
- Releases are built by GitHub Actions from the public source.

### Nothing appears when I start it

The app in a given folder runs as a single instance and minimizes to the notification area (tray) when you close its window. To bring the window back, either:

- click the tray icon, or
- start the app again; the window that's already running comes to the front.

To quit when the window is closed instead, choose "Quit the app" under Settings → When the main window is closed.

### Resetting the settings

Quit the app (tray icon → Quit), then delete `data\settings.json` in the app's folder.

If the settings file is ever unreadable, the app renames it to `settings.json.corrupt` as a backup and starts with the defaults.

### The Remote Desktop title bar shows 127.x.y.z

That's expected. mstsc connects to the local tunnel entrance, so that's the only address it knows. Each connection's loopback address never changes, so the passwords and certificate trust mstsc remembers stay separate for each computer.

### Start with the Diagnostics page

The Diagnostics page in the sidebar lists what on this computer affects Remote Desktop connections: the versions of Windows, Remote Desktop Connection (mstsc) and WebView2; the RD Gateway, server authentication and "Always ask for credentials" settings in Remote Desktop's defaults (Default.rdp) and the policies over them; and the policies that decide whether saved passwords may be used. It only reads; it changes nothing. Items that may get in the way are marked yellow or red, with where to change them.

When reporting a problem, click "Copy diagnostics" and paste the text into the issue: it leaves out server names and folder paths.

### Where do I set clipboard, drives and sound?

The app starts Remote Desktop with `mstsc /v:` (since April 2026, opening an .rdp file shows a security warning every time). That way only the display (full screen, window size, monitors) and `/admin` can differ per connection; everything else is the same for every connection and comes from `Default.rdp` in Documents.

To change it, click "Change Remote Desktop's default settings" on the Diagnostics page or in a connection's editor. Remote Desktop Connection opens; change the Display, Local Resources, Experience and Advanced tabs, then go back to the General tab and click Save. Without Save, the changes are not kept.

### Importing a connection from an .rdp file

"Import .rdp" on the Connections page reads the file's computer address and port, user name (with its `domain`), display settings and administrative session into the form for a new connection; check them and save, then choose the proxy in the list. The file's clipboard, drives and similar settings are not imported (see above). If the file connects through an RD Gateway, you're told so: the imported connection reaches the computer directly through the proxy instead. You're also told when a connection to the same computer already exists.

### Reading the route check

A connection's More → Check route does two things at once, without starting Remote Desktop:

- **Proxy**: fetches the test URL from Settings through the proxy, to see that the proxy itself works. Skipped for a direct connection.
- **Remote computer**: asks the Remote Desktop service on the remote computer to answer through the proxy, then shows the security it requires, such as HYBRID_EX for Network Level Authentication (NLA).

Together they show where a problem is: if the proxy works but the remote computer doesn't answer, the problem lies beyond the proxy (the computer's address or port, the computer being off or not allowing Remote Desktop, a firewall); if both fail, it's most likely the proxy itself (server, port, user ID or password); if the remote computer answers but the test URL fails, Remote Desktop is not affected. The check has no time limit: it waits as long as you do, and you can cancel it any time.

### Remote Desktop asks for the password although one is saved

See the "Saved passwords" section of the Diagnostics page:

- **Saved passwords through the tunnel: Not allowed.** Through the tunnel mstsc connects to `127.x.y.z`, a name with no Kerberos identity, so the server can be authenticated with NTLM only, and a computer in a domain does not use saved passwords then by default; Remote Desktop says your system administrator does not allow saved credentials. An administrator has to enable Computer Configuration → Administrative Templates → System → Credentials Delegation → "Allow delegating saved credentials with NTLM-only server authentication" with `TERMSRV/*`. The app doesn't change policies.
- **Always ask for credentials: Yes.** Remote Desktop's defaults have "Always ask for credentials" ticked, so saved passwords aren't filled in. Untick it on the General tab of "Change Remote Desktop's default settings" and save. The session log also mentions it when you connect.
- **Credential Guard: Running.** A password Remote Desktop saved itself ("Remember me" in its sign-in box) may not be used; one saved with "Remember the password" in this app's connection is not affected.

### Remote Desktop warns about the certificate, or can't verify the remote computer

Through the tunnel mstsc connects to `127.x.y.z`, which never matches the name on the remote computer's certificate, so the first connection to each computer shows a certificate warning. Once you're sure it's the right computer, tick "Don't ask me again for connections to this computer" and connect; the warning doesn't come back (the trust is remembered per connection's loopback address).

If Remote Desktop refuses to connect at all, check "If server authentication fails" on the Diagnostics page: with "Do not connect", it refuses unless you trusted the computer's certificate before. Change it to "Warn me" on the Advanced tab of "Change Remote Desktop's default settings" and save. If the Group Policy "Configure server authentication for client" sets it, only an administrator can change it.

### "Remote Desktop is set to always connect through an RD Gateway"

An RD Gateway reaches the target from its own side of the network and can't reach this computer's tunnel entrance, so connections through the app must not use one. In "Change Remote Desktop's default settings" → Advanced → "Connect from anywhere" → Settings, choose "Do not use an RD Gateway server" or "Automatically detect RD Gateway server settings", then click Save on the General tab. The Diagnostics page shows the current gateway setting. A gateway enabled by Group Policy is only used when a direct connection fails, so the app just notes it in the session log.

### "Windows does not let this app use the local port"

Hyper-V, WSL, Docker and others reserve port ranges, and the app can't listen on those ports. Choose another port under Settings → Connections → Local entrance port (such as 23389); it applies to the next connections. `netsh interface ipv4 show excludedportrange protocol=tcp` lists the reserved ranges.

### Where's the log file?

It's `logs\app.log` in the app's folder (both the Data section of the Settings page and the Diagnostics page open the log folder). When it reaches 2 MB it's renamed to `app.1.log`; at most two older files are kept.

Before writing a line, the app replaces the host names, proxy server addresses, user names and connection and proxy names you entered, and proxies' user IDs, server names (SNI), paths and keys, with `<redacted>`, your user folder (its path contains your Windows account name) with `%USERPROFILE%`, and every IP address other than this computer's loopback addresses with `<ip>`, so the log can be attached to an issue. Passwords never go into the log. Please still look it over before you attach it.

### Proxy passwords are gone after moving to another PC or Windows user

Proxy passwords (and the user IDs and transport and TLS settings of VMess and VLESS proxies) are encrypted in their files with Windows DPAPI for the current user, and only the same Windows user can decrypt them. If you copy the app's folder to another PC, or run it as another Windows user, the proxies and connections are all there, but these have to be entered again (pasting the share links again is quickest). The Proxies page marks such proxies "Needs re-entering", and they aren't used until saved again: what's left are default settings, and connecting with them could send a user ID without the encryption it was meant to travel in. The app notes which files are affected when it starts (in the log, and in the window once the connection pages are done).

If a file is damaged and can't be read, the app renames it to `.corrupt` and keeps it; everything else loads as usual.

### Pasting a share link says Xray no longer has the h2 / quic transport

Xray has removed the HTTP/2 (h2) and QUIC transports, so such servers can't be reached with Xray. Ask the server's provider to switch to XHTTP, WebSocket, gRPC or another transport.

### "The link asks to skip certificate checks", or the certificate didn't pass when connecting

Since June 2026 Xray no longer skips certificate verification (`allowInsecure`, `insecure=1`). The app imports such links and checks the certificate as usual:

- A server with a proper certificate connects as before.
- For a self-signed certificate, enter its SHA-256 hash under the proxy's "Certificate fingerprint (SHA-256)"; the app then trusts exactly that certificate. The provider can give you the hash; Hysteria2 links often carry it as `pinSHA256`, which is filled in on import.

With REALITY, this usually means the public key (pbk) or short ID is wrong.

### A VMess, VLESS or Trojan proxy only says "The connection closed before the remote computer answered"

Servers of these protocols don't tell a client what's wrong: with a wrong user ID or password they just close the connection (a VMess server waits a while first). A connection's Check route tells the cases apart: if its Proxy step fails too, the user ID or password is most likely wrong; if Proxy passes and Remote computer fails, the problem lies beyond the proxy.

If a VMess link has an alterId other than 0, the import notes that it was ignored: Xray speaks only VMess AEAD. A server that still requires the old authentication needs its settings changed.
