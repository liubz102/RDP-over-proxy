# RDP over Proxy

[中文](README.md) | [English](README.en.md)

让 Windows 自带的远程桌面（mstsc）走代理：支持 SOCKS5、HTTP，以及 VMess、VLESS、Trojan、Shadowsocks、Hysteria2。

> **开发中**：当前处于 0.1.0 预览阶段。已经可以经 SOCKS5、HTTP 和 V2Ray 系代理连接远程桌面，也能导入 .rdp 文件、查看环境诊断；安装包等还在开发。进度见 [docs/PROGRESS.md](docs/PROGRESS.md)。

## 为什么需要它

跨地区、线路差的时候，远程桌面会很卡。如果你有一台线路好的代理服务器，让远程桌面走代理往往能明显改善。

问题在于 mstsc 本身不支持任何代理，只支持微软的 RD 网关。常见的变通办法有两种：
- 手写 v2ray 配置做端口转发；
- 使用 TUN 模式或 Proxifier 这类工具，但它们需要管理员权限，还会接管整机流量。

RDP over Proxy 专门解决这件事：

- 使用 Windows 自带的 mstsc，画质、多显示器、剪贴板等体验完全不变。
- 按目标电脑保存连接配置。代理单独管理，多个连接可以共用同一个代理。
- 内置主流代理协议，粘贴分享链接即可导入。
- 不需要管理员权限，不安装驱动，不改系统路由，只影响远程桌面这一个程序。

## 工作原理

```
mstsc ──▶ 127.x.y.z:13389 ──▶ RDP over Proxy（本机隧道）──▶ 代理服务器 ──▶ 目标电脑
```

每个连接在本机分配一个固定的回环地址 `127.x.y.z`。mstsc 连接这个地址，隧道再通过你选择的代理把流量送到目标电脑。代理协议由内嵌的 [Xray-core](https://github.com/XTLS/Xray-core) 实现，不需要另外运行 v2ray 或 Xray。

## 功能

已完成（✓）和规划中：

- ✓ 中英双语界面，首次启动时选择语言，之后可以在设置里修改
- ✓ 关闭窗口时缩到托盘、单实例运行、跟随系统深浅色
- ✓ 连接管理：目标地址和端口、使用的代理、登录用户名、记住密码（保存在 Windows 凭据管理器）
- ✓ 代理管理：SOCKS5、HTTP、VMess、VLESS（含 REALITY）、Trojan、Shadowsocks、Hysteria2、自定义 Xray 出站
- ✓ 粘贴分享链接导入，包括 v2rayN 导出的链接；也能复制代理的分享链接
- ✓ 连接前检查线路：通过代理向目标发送 RDP 握手，报告延迟和对方支持的安全协议
- ✓ 分步检查线路：分别测代理本身和经代理的远程计算机，指出问题出在哪一段
- ✓ 一键连接；关闭远程桌面窗口后，隧道自动关闭
- ✓ 从 .rdp 文件导入连接
- ✓ 诊断页：Windows 和远程桌面的版本、远程桌面默认设置里会影响连接的项、保存的密码能不能用；可以复制去掉了服务器名称的诊断信息
- ✓ 全部数据保存在程序所在的文件夹里，不写用户目录
- ✓ 自动发现本机正在运行的 v2rayN、Clash 等代理软件，一键添加为代理
- ✓ 代理可以选「跟随系统代理」：按 Windows 的代理设置连接（手动代理和例外列表、自动配置脚本），没有设置代理时直连
- ✓ 在程序里查看日志，实时更新，可按级别筛选、搜索
- 安装包

## 系统要求

- Windows 10 或 11（64 位）
- Microsoft Edge WebView2 运行时：Windows 11 自带；大多数 Windows 10 已随 Edge 一起安装

## 数据保存在哪里

全部在程序所在的文件夹里，打开就能看到：

```
RDP-over-proxy\
├─ RDP-over-proxy.exe
├─ data\      设置、代理、连接，以及界面的缓存（WebView2）
└─ logs\      日志
```

请把程序放在可以写入的文件夹里（例如 `D:\Tools\RDP-over-proxy`）。放进 `C:\Program Files` 这类需要管理员权限的地方时，程序第一次启动会说明原因并请求一次管理员权限，只用来建好这两个文件夹。

代理的密码在文件里用 Windows 加密，只有这台电脑上的同一个 Windows 用户能解开。远程桌面的密码保存在 Windows 凭据管理器里，因为「远程桌面连接」只从那里读取。

## 从源码构建

需要 Go 1.27、Node.js 24，以及 Wails v3 命令行工具：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27
wails3 build      # 生成 bin\RDP-over-proxy.exe
wails3 dev        # 开发模式，支持热重载
```

更多开发说明见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 常见问题

**为什么杀毒软件或 SmartScreen 会报警？**
程序没有代码签名，又内置了代理核心，部分安全软件会误报。所有发布版本都由 GitHub Actions 从公开源码构建，并附带 SHA256 校验值。

**为什么远程桌面窗口标题里有 127.x.y.z？**
标题最前面是连接的名称，后面的 127.x.y.z 是 mstsc 实际连接的本机隧道入口：mstsc 不知道代理的存在，只知道这个地址。每个连接的回环地址是固定的，因此 mstsc 记住的密码和证书信任不会在不同电脑之间混用。

更多问题见 [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)。

## 许可证

本项目使用 [MIT 许可证](LICENSE)。

代理协议由 [Xray-core](https://github.com/XTLS/Xray-core)（MPL-2.0）提供，以未经修改的库形式使用。第三方组件的许可证信息随发布版本一起提供。

## 致谢

[Xray-core](https://github.com/XTLS/Xray-core) · [Wails](https://wails.io) · [Fluent UI](https://react.fluentui.dev)
