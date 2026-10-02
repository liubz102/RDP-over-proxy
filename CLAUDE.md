# CLAUDE.md —— RDP over Proxy

**开工先读 `docs/PROGRESS.md`**：里面有当前里程碑、下一步、待用户回答的问题。**收工前更新它**。

- 架构与接口见 `docs/ARCHITECTURE.md`。
- 用户批准的总计划已经整理进 `docs/ARCHITECTURE.md` 和 `docs/PROGRESS.md`。

## 项目一句话

让 Windows 的 mstsc 走代理。每个连接分配一个固定的本机回环地址 `127.x.y.z` 作为隧道入口，mstsc 用 `mstsc /v:` 连接它；隧道再经 SOCKS5、HTTP 或 V2Ray 系代理（内嵌 Xray-core）到达目标。只支持 Windows。

## 技术栈与锁定版本

| 组件 | 版本 | 备注 |
|---|---|---|
| Go | 1.27.0 | winget 安装在 `C:\Program Files\Go` |
| Wails v3 | v3.0.0-beta.27 | Go module 与 npm `@wailsio/runtime` 必须是同一个版本，要升级就一起升 |
| wails3 CLI | v3.0.0-beta.27 | `%USERPROFILE%\go\bin\wails3.exe` |
| React / Fluent UI v9 / i18next / zustand | 19.3 / 9.74 / 26 / 5 | |
| TypeScript | 6.0.x | 暂不用 7.x（原生重写版，生态未跟上） |
| Xray-core | v1.260327.0 | 最后一个稳定的 module tag，之后的版本都是预发布。它要求 Go ≥ 1.26，所以 `go.mod` 的 `go` 指令是 1.26 |

## 目录地图

| 路径 | 内容 |
|---|---|
| `main.go` | 入口；嵌入 `frontend/dist` 和 `build/appicon.png`；`version` 默认值 |
| `internal/app` | Wails 应用、窗口、托盘、单实例、关闭缩到托盘。`desktop_windows.go` 和 `server.go` 用构建标签区分桌面版与 server 版 |
| `internal/api` | 暴露给前端的服务（目前只有 `SettingsService`），以及事件 `settings:changed` |
| `internal/model` | 数据结构（Settings、Proxy、Profile、Target）与校验，纯逻辑。Proxy / Profile 的校验返回 `FieldErrors`（字段 + 代码） |
| `internal/loopback` | 由 profile ID 派生 `127.a.b.c` 回环地址，冲突时顺延 |
| `internal/rdpfile` | 只读解析 .rdp：导入草稿、RD 网关判定 |
| `internal/mstsc` | mstsc 启动参数（`Args`，纯函数）；启动 / 等待 / 关闭 / 结束 / 聚焦（`launch_windows.go`） |
| `internal/probe` | X.224 CR/CC 编解码、线路检查 `Check` |
| `internal/route` | `Dialer` / `Provider` 接口、直连 |
| `internal/engine` | 内嵌的 Xray 实例，也是应用实际使用的 `route.Provider`：outbound 引用计数、强制 tag 派发、错误原因回传、日志桥接 |
| `internal/tunnel` | 回环入口：接受连接、经线路拨目标、双向拷贝、计数、报告 |
| `internal/session` | 会话：纯 reducer（`session.go`、`reduce.go`），外壳是 actor（`actor.go`）和 `Manager`（`manager.go`） |
| `internal/testutil` | 测试共用：假 RDP 服务端；替身进程（`RunHelper` / `HelperCommand`）；`FreePort`。只能被 `_test.go` 引用 |
| `internal/testutil/xraytest` | 测试用：进程内的 Xray SOCKS / HTTP 代理。单独成包，只有需要的测试才链接 Xray |
| `internal/store` | 原子写 JSON；数据目录；`RDP_OVER_PROXY_HOME` |
| `internal/i18n` | Go 侧文案（托盘、原生对话框）、系统语言检测 |
| `internal/winx` | Win32 调用：WebView2 检测、错误框、系统深色模式 |
| `frontend/src` | `app/`（外壳、主题、首次语言选择）、`features/`、`components/`、`stores/`、`locales/` |
| `frontend/bindings` | `wails3 generate bindings` 生成，不要手改 |
| `build/` | Wails 构建配置，只保留 Windows |
| `legacy/` | 旧脚本原型。已被 git 忽略，新版功能对等且用户确认后才删除 |

## 常用命令（PowerShell）

工具里的 shell 可能是装 Go 之前的环境，先执行：

```powershell
$env:Path = 'C:\Program Files\Go\bin;' + "$env:USERPROFILE\go\bin;" + $env:Path
```

- **构建**
  - 正式构建：`wails3 build`，产出 `bin\RDP-over-proxy.exe`
  - 开发模式：`wails3 dev`
- **测试与检查**
  - Go 测试：`go test ./internal/...`
  - Go 全量检查：`go vet ./...`。`main` 包嵌入了 `frontend/dist`，所以要先构建一次前端
  - 前端：`npm --prefix frontend run typecheck`、`npm --prefix frontend test`
- **重新生成绑定**：`wails3 generate bindings -clean=true -ts -i`
- **浏览器预览界面**
  1. 运行 `wails3 task build:server DEV=true`。
  2. 设置 `WAILS_SERVER_PORT=34115`，后台运行 `bin\RDP-over-proxy-server.exe`。
  3. 在内置浏览器打开 `http://localhost:34115`。

  预览版的数据放在 `%APPDATA%\RDP-over-proxy-preview`。
- **原生自测**：设置 `RDP_OVER_PROXY_HOME=<临时目录>` 后再启动 exe，避免用掉用户的首次启动体验。

## 硬性规则（用户的全局规则 + 本项目约定）

1. **git**：不执行任何改变 git 状态或履历的命令（init、commit、push、tag、stash、reset 等）。只读的 status、log、diff 可以用。
2. **版本号**：不擅自修改。当前的 0.1.0 是用户定的，出现在以下位置：
   - `build/config.yml`、`build/windows/info.json`、`build/windows/wails.exe.manifest`
   - `frontend/package.json`、`main.go`
3. **时序逻辑必须事件驱动**：不写 sleep，不写固定次数重试，不设拍脑袋的超时。已登记的例外都要在代码里注释原因：
   - Xray `connIdle` 调到最大（M3）
   - 只用于显示的计时器
   - 测试里的空闲端口辅助函数
   - 测试脚手架的兜底超时
4. **.bat**：不写。如果非写不可，用纯 ASCII + CRLF，中文放进同名 `.ps1`（UTF-8 带 BOM）。
5. **测试纪律**：
   - 测试只用自己的端口，隧道用 port 0，绝不用 13389。
   - 只结束测试自己启动的 PID，绝不按进程名杀进程。
   - 不替用户打开真实的 mstsc 会话。
   - 测试和自测的数据用 `RDP_OVER_PROXY_HOME` 隔离。
6. **隐私**：仓库和日志里不出现个人主机名、IP、凭据。测试数据只用 `example.com` 和 `192.0.2.x`。
7. **`wails3 init`**：绝不加 `-git`，它会执行 git init 和 add。
8. **先征得同意**：安装软件、移动用户的文件、碰真实凭据之前，先问用户。
9. **双语**：
   - 界面文案只放在 `frontend/src/locales/{zh-CN,en}.json`（有测试强制两边 key 一致）和 `internal/i18n`。
   - README、CONTRIBUTING、SECURITY、TROUBLESHOOTING、Issue 模板都要同时维护中英两版。

## 已知的坑

- **FluentProvider 不能传 `className`**：Fluent 会把它复制到所有弹出层的 portal 节点上，曾导致下拉框变成全屏白板。根元素高度在 `styles.css` 里用 `#root > .fui-FluentProvider` 设置。
- **Fluent 的 `Text` 系组件自带 `text-align`**：要居中得传 `align="center"`。
- **托盘菜单改文字后要重新 `tray.SetMenu(menu)`**：`Menu.Update()` 刷不到托盘的弹出菜单。
- **托盘右键菜单在 Wails v3 beta 上可能弹不出来**（#6161）：所以左键点击打开主窗口，所有功能都在窗口里。
- **Wails server 模式不支持单实例**，会直接报错，这也是 `internal/app` 要用构建标签区分的原因。
- **`wails3 generate icons` 要传 `-macfilename ""`**：否则会去写 `build/darwin/`，而本仓库已经删掉了这个目录。
- **`build/windows/info.json` 是手工改过的**：改成了 0409 字符串表，并加上固定的产品版本。运行 `wails3 task common:update:build-assets` 会按模板覆盖它，覆盖后要改回来。
- **Task 并行执行依赖任务**：一个失败会取消其他任务，别的任务可能报出误导性的错误（比如 "npm isn't installed"）。要找第一个真正的错误。
- **读取 JSON 配置时先用默认值填好结构体再解码**（见 `store.SettingsStore.Load`）：否则后来新增的 bool 字段会被读成 false。
- **`core.Dial`（Xray，M3）是异步的**：失败通过 `session.TrackedConnectionError` 事件送达；目标地址非法时会 panic，要先校验。
- **2026-04 起，打开 .rdp 文件每次都会弹安全对话框**：所以只用 `mstsc /v:` 直连模式（用户已拍板）。剪贴板等设置沿用 mstsc 的全局 `Default.rdp`。
- **Windows 上计时可能读到 0**：单调时钟的精度比本机回环往返还粗，测试里不要断言耗时大于 0。
- **`probe` 的测试分两个包**：编解码测试是 `package probe`；用到 `testutil` 的 `Check` 测试必须是 `package probe_test`，否则会循环导入（`testutil` 依赖 `probe`）。
- **本机跑不了 `-race`**：没有 gcc。
- **Windows 上 `Wait` 之后再 `os.Process.Kill`，返回的是 `EINVAL`，不是 `ErrProcessDone`**：`mstsc.Process` 因此改用自己持有的句柄调 `TerminateProcess`。
- **`windows.NewCallback` 创建的回调释放不掉，数量也有上限**：只能在包级变量里创建一次（见 `winx` 的 `enumCallback`），不要在函数里每次新建。
- **用替身进程的测试包必须有 `TestMain`，并且第一行调用 `testutil.RunHelper()`**：否则子进程会把整套测试再跑一遍。`HelperCommand` 带了 `-test.run=^$` 作为兜底。
- **会话 / 隧道的回调里不能阻塞**：`tunnel.Reporter` 的方法不能等任何东西，因为 `Close` 要等它们返回；`Manager` 的 `Changed` / `Log` 回调里不能调 `Quit`。
- **Xray 的几个行为**（engine 已经处理，改动 engine 时要记得）：
  - 日志处理器是进程级全局的：每次 `core.New` 都会注册 Xray 自己的那个，所以 engine 在 `core.New` 之后才注册桥接。
  - 第一个加入的 outbound 会成为默认出站：所以 base 配置里先放 blackhole。
  - `RemoveHandler` 只是把 outbound 从表里删掉，不会关闭它：要自己调 `Close`。
  - `Dispatch` 遇到非法目标会 panic：拨号前必须先校验地址。
- **测试要看引擎日志时，先启动 `xraytest` 代理，再启动引擎**：创建 Xray 实例会顶掉日志桥接。
- **测「代理不通」的用例要等约 1.5 秒**：这是 Xray 内部的重试，不是我们的超时。
- **PowerShell 命令开头那行 PATH 设置里有 `C:\Program Files`，同一条命令里再写 `Remove-Item` 会被工具的安全检查拦下**：删除操作单独一条命令执行，或者改用 Bash。
