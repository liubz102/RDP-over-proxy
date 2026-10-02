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

写入日志前，程序会把你填写过的主机名、代理服务器地址、用户名、连接和代理的名称替换成 `<redacted>`，把用户目录（路径里有你的 Windows 账户名）替换成 `%USERPROFILE%`，把除本机回环地址以外的 IP 地址替换成 `<ip>`，方便附到 Issue 里。密码不会写进日志。附上之前仍请自己检查一遍。

### 换了电脑或 Windows 用户后，代理的密码没了

代理的密码在文件里是用 Windows 的 DPAPI 按当前用户加密的，只有同一个 Windows 用户才能解开。把 `%APPDATA%\RDP-over-proxy` 复制到别的电脑或别的用户下，代理和连接都还在，但代理的密码需要重新填写。程序启动时会记下是哪些文件（写在日志里，界面完成后也会在窗口里提示）。

如果某个文件损坏、无法读取，程序会把它改名为 `.corrupt` 保留下来，其余的照常载入。

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

Before writing a line, the app replaces the host names, proxy server addresses, user names and connection and proxy names you entered with `<redacted>`, your user folder (its path contains your Windows account name) with `%USERPROFILE%`, and every IP address other than this computer's loopback addresses with `<ip>`, so the log can be attached to an issue. Passwords never go into the log. Please still look it over before you attach it.

### Proxy passwords are gone after moving to another PC or Windows user

Proxy passwords are encrypted in their files with Windows DPAPI for the current user, and only the same Windows user can decrypt them. If you copy `%APPDATA%\RDP-over-proxy` to another PC or user, the proxies and connections are all there, but the proxy passwords have to be entered again. The app notes which files are affected when it starts (in the log, and in the window once the connection pages are done).

If a file is damaged and can't be read, the app renames it to `.corrupt` and keeps it; everything else loads as usual.
