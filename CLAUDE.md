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
| `main.go` | 入口；嵌入 `frontend/dist` 和 `build/windows/icon.ico`（托盘图标）；`version` 默认值 |
| `internal/app` | Wails 应用、窗口、托盘、单实例、关闭缩到托盘、启动与退出顺序。`desktop_windows.go` 和 `server.go` 用构建标签区分桌面版与 server 版；`platform_windows.go` 提供两版共用的 Windows 部件（DPAPI、凭据、mstsc）；`folders_windows.go`（桌面版）：启动时先准备 exe 旁的 `data`、`logs`，只有管理员能写时经 UAC 让提权的自己（`--prepare-folders <SID>`）建好并授权，其他问题弹中英双语的说明 |
| `internal/api` | 暴露给前端的服务（Settings、Profile、Proxy、Session、App）和它们共用的 `Core`；视图类型、事件、错误 JSON（`MarshalError`） |
| `internal/model` | 数据结构（Settings、Proxy、Profile、Target）与校验，纯逻辑。Proxy / Profile 的校验返回 `FieldErrors`（字段 + 代码）。V2Ray 系的设置在 `ProxyOptions`（`options.go`），`Normalize` 只留下这种代理用得上的项并补默认值 |
| `internal/loopback` | 由 profile ID 派生 `127.a.b.c` 回环地址，冲突时顺延 |
| `internal/rdpfile` | 只读解析 .rdp：导入草稿、RD 网关判定、服务器身份验证和「始终要求凭据」 |
| `internal/mstsc` | mstsc 启动参数（`Args`，纯函数）；启动 / 等待 / 关闭 / 结束 / 聚焦，窗口标题前加连接名（`ShowName`，格式见纯函数 `Title`）（`launch_windows.go`）；`Servers` 注册表记忆（UsernameHint）；Default.rdp 和网关策略（`DecideGateway`、`DecideDefaults` 纯函数 + `ReadDefaults`）；`EditDefaults`（`mstsc /edit Default.rdp`） |
| `internal/probe` | X.224 CR/CC 编解码、线路检查 `Check` |
| `internal/route` | `Dialer` / `Provider` 接口、直连 |
| `internal/engine` | 内嵌的 Xray 实例，也是应用实际使用的 `route.Provider`：outbound 引用计数、强制 tag 派发、错误原因回传、日志桥接；各类代理的出站生成（`outbound.go`）和保存前的 `Check` |
| `internal/sharelink` | 分享链接（vmess、vless、trojan、ss、hysteria2、socks、http）⇄ `model.Proxy`，纯逻辑 |
| `internal/tunnel` | 回环入口：接受连接、经线路拨目标、双向拷贝、计数、报告 |
| `internal/session` | 会话：纯 reducer（`session.go`、`reduce.go`），外壳是 actor（`actor.go`）和 `Manager`（`manager.go`） |
| `internal/errcode` | 错误码：`New` / `Weak` / `Wrap`，`Of` 取最有用的代码；`Declare` / `All` 供翻译完整性测试 |
| `internal/secret` | DPAPI 加密（`DPAPI`）；凭据管理器里 `TERMSRV/<回环地址>` 的密码（`Vault`） |
| `internal/logging` | 日志文件（按大小轮转）、环形缓冲、脱敏、连续重复折叠、给 Wails 用的 slog 适配 |
| `internal/store` | 原子写 JSON；数据文件夹：exe 旁的 `data`、`logs`（`DefaultDirs`，`RDP_OVER_PROXY_HOME` 代替 exe 所在文件夹），`Dirs.Prepare` 建好并试写；设置；代理和连接的文件存储（`Data`） |
| `internal/i18n` | Go 侧文案（托盘、原生对话框）、系统语言检测；`Both`：语言设置还读不到时用的中英双语文案 |
| `internal/winx` | Win32 调用：WebView2 检测和版本、错误框、确认框、系统深色模式、窗口；窗口标题（`Title`、`SetTitle`）和盯着一个进程的窗口事件（`WatchWindows`，WinEvent 钩子，`watch_windows.go`）；文件版本、Credential Guard（WMI）、打开文件夹（`system_windows.go`）；提权（`elevate_windows.go`）：`Elevated`、`RunElevated`、`AllowModify`、`OnLocalDisk`；`ErrorText`（按界面语言取 Windows 的错误说明） |
| `internal/diag` | 只读的环境报告：`Gather` 读 Windows，`Build`（纯函数）生成报告项；凭据委派策略的判定（`delegation.go`） |
| `tests/<包名>` | Go 测试，每个被测包一个目录（如 `tests/session`），包名 `<包名>_test`，只用导出的 API；`tests/rdpfile/testdata` 是 .rdp 样本 |
| `tests/winx`、`tests/diag` | 除了纯逻辑，还有读本机 Windows 的测试（文件版本、WMI、`diag.Gather`），只读 |
| `tests/testutil` | 测试共用：假 RDP 服务端；替身进程（`RunHelper` / `HelperCommand`，其中 `HelperTitledWindows` 有三个带标题的窗口，测窗口标题用）；`FreePort`。只能被测试引用 |
| `tests/testutil/xraytest` | 测试用：进程内的 Xray 代理服务端，SOCKS / HTTP 和 V2Ray 系各协议、各传输、TLS / REALITY。按客户端设置起对应的服务端，`Model` 填上证书指纹和 REALITY 公钥。单独成包，只有需要的测试才链接 Xray |
| `frontend/src` | `app/`（外壳、主题、首次语言选择）、`features/`、`components/`、`stores/`、`locales/` |
| `frontend/tests` | 前端测试（vitest），目录结构和 `frontend/src` 对应 |
| `frontend/bindings` | `wails3 generate bindings` 生成，不要手改 |
| `build/` | Wails 构建配置，只保留 Windows |
| `build/icon` | 应用图标的源文件：`appicon.svg`（96px 及以上）和逐像素对齐重画的 `appicon-<尺寸>.svg`（16–64px）。`generate.go`（`//go:build ignore`）用 Edge 无头模式把它们画成 `build/appicon.png` 和 `build/windows/icon.ico`，生成结果入库 |
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
  - Go 测试：`go test ./...`（测试都在 `tests/` 下）
  - Go 全量检查：`go vet ./...`。`main` 包嵌入了 `frontend/dist`，所以要先构建一次前端
  - 前端：`npm --prefix frontend run typecheck`、`npm --prefix frontend test`
- **重新生成绑定**：`wails3 generate bindings -clean=true -ts -i`
- **重新生成图标**：改完 `build/icon` 里的 SVG 后运行 `wails3 task common:generate:icons`（要有 Edge），生成的 `build/appicon.png`、`build/windows/icon.ico` 一起提交。构建不会自动生成。
- **浏览器预览界面**
  1. 运行 `wails3 task build:server DEV=true`。
  2. 用内置浏览器工具 `preview_start` 启动 `.claude/launch.json` 里的 `preview` 配置：端口 34115，`RDP_OVER_PROXY_HOME` 是仓库里的开发目录 `data\preview`（已被 git 忽略），所以预览的数据在 `data\preview\data`、日志在 `data\preview\logs`。开头那个 `data\` 是仓库的开发目录，不是程序的数据文件夹，给用户看截图时要说明。
  3. 页面在 `http://localhost:34115`。

  没有界面的服务可以在页面里用 `fetch("/wails/runtime")` 按方法全名调用（`object: 0`，`args: {"call-id", methodName, args}`；取消用 `object: 10`）。不经过 launch.json 手动运行时，预览版的数据放在 exe 旁的 `bin\data-preview`、`bin\logs-preview`，和桌面版的 `bin\data`、`bin\logs` 分开。
- **原生自测**：把 exe 复制到临时文件夹再启动（数据就在它旁边），或者设置 `RDP_OVER_PROXY_HOME=<临时目录>`。
- **`bin\data`、`bin\logs` 是用户自己在用的数据**（2026-10-06 从 `%APPDATA%` 挪过来，用户选的位置）：直接运行 `bin\RDP-over-proxy.exe` 或 `wails3 dev`（它构建并运行的就是这个 exe）都会读写它们，自测不要这样跑，更不要删它们。用户的程序从 `bin\` 运行时，`wails3 build` 覆盖不了正在运行的 exe，会构建失败：请用户先退出，不要结束用户的进程。

## 硬性规则（用户的全局规则 + 本项目约定）

1. **git**：不执行任何改变 git 状态或履历的命令（init、commit、push、tag、stash、reset 等）。只读的 status、log、diff 可以用。
2. **版本号**：不擅自修改。当前的 0.1.0 是用户定的，出现在以下位置：
   - `build/config.yml`、`build/windows/info.json`、`build/windows/wails.exe.manifest`
   - `frontend/package.json`、`main.go`
3. **时序逻辑必须事件驱动**：不写 sleep，不写固定次数重试，不设拍脑袋的超时。已登记的例外都要在代码里注释原因：
   - Xray `connIdle` 调到最大（M3）
   - Hysteria2 的 QUIC 保活心跳，每 10 秒（M6，`engine.quicKeepAlive`）：QUIC 静默 30 秒就断，NAT 也会忘掉空闲的 UDP 映射，空闲的连接上没有事件可等
   - 只用于显示的计时器
   - 测试里的空闲端口辅助函数（`FreePort`、`FreeUDPPort`）
   - 测试脚手架的兜底超时；测试用 Xray 服务端的握手时限 4 秒（`xraytest`，Xray 默认 60 秒，错误 VMess ID 的用例要等满）
4. **.bat**：不写。如果非写不可，用纯 ASCII + CRLF，中文放进同名 `.ps1`（UTF-8 带 BOM）。
5. **测试纪律**：
   - 测试只用自己的端口，隧道用 port 0，绝不用 13389。
   - 只结束测试自己启动的 PID，绝不按进程名杀进程。
   - 不替用户打开真实的 mstsc 会话。
   - 测试和自测的数据要隔离：exe 复制到临时文件夹，或者用 `RDP_OVER_PROXY_HOME`。单实例 ID 按数据文件夹区分，所以自测不会和用户正在用的实例互相干扰。
   - 自测不要点提权说明框的「确定」（会弹 UAC），也不要往 Program Files 放东西：提权说明框和错误框可以用测试脚本读窗口文字后结束进程来验证；`--prepare-folders <自己的 SID>` 可以不提权直接在临时文件夹里跑。
   - 不碰用户真实的凭据和 mstsc 注册表：凭据测试只用 `TERMSRV/rdp-over-proxy-test-<随机>.invalid`，注册表测试只用 `HKCU\Software\RDP-over-proxy-test`，测试结束都要清掉。
   - 自测时不要往凭据管理器存密码，也不要调用 `Connect`（会启动 mstsc）。
6. **隐私**：仓库和日志里不出现个人主机名、IP、凭据。测试数据只用 `example.com` 和 `192.0.2.x`。
7. **`wails3 init`**：绝不加 `-git`，它会执行 git init 和 add。
8. **先征得同意**：安装软件、移动用户的文件、碰真实凭据之前，先问用户。
9. **测试和代码分开放**（用户要求，2026-10-03）：
   - Go 测试放 `tests/<包名>/`，前端测试放 `frontend/tests/`，目录结构镜像源码。`internal/`、`frontend/src` 里不放任何测试文件、测试数据或测试辅助代码。
   - Go 测试是外部测试包（`package <包名>_test`），只用导出的 API。原来写在包内的测试用点导入（`import . "…/internal/<包名>"`），正文不用加包名。
   - 新代码要设计成能通过公开 API 测到（依赖用接口注入，见 `api.Deps`、`session.Options`）。不要为了测试导出内部细节；确实需要的检查清单按 `session.Messages`、`i18n.Keys` 的模式提供。
10. **双语**：
   - 界面文案只放在 `frontend/src/locales/{zh-CN,en}.json`（有测试强制两边 key 一致）和 `internal/i18n`。
   - README、CONTRIBUTING、SECURITY、TROUBLESHOOTING、Issue 模板都要同时维护中英两版。

## 已知的坑

- **FluentProvider 不能传 `className`**：Fluent 会把它复制到所有弹出层的 portal 节点上，曾导致下拉框变成全屏白板。根元素高度在 `styles.css` 里用 `#root > .fui-FluentProvider` 设置。
- **Fluent 的 `Text` 系组件自带 `text-align`**：要居中得传 `align="center"`。
- **托盘菜单改文字后要重新 `tray.SetMenu(menu)`**：`Menu.Update()` 刷不到托盘的弹出菜单。
- **托盘右键菜单在 Wails v3 beta 上可能弹不出来**（#6161）：所以左键点击打开主窗口，所有功能都在窗口里。
- **Wails server 模式不支持单实例**，会直接报错，这也是 `internal/app` 要用构建标签区分的原因。
- **不要用 `wails3 generate icons`**：它把一张 PNG 缩放成各个尺寸，会盖掉逐像素对齐的 16–64px 小图（缩出来的边框落在半像素上，发虚），也没有 20、24、40 这几个尺寸。图标只用 `wails3 task common:generate:icons` 生成，构建也不再自动重建 `icon.ico`。改 `appicon.svg` 时，`appicon-<尺寸>.svg` 要跟着手工改。
- **托盘图标传的是 `icon.ico`，不要换回 PNG**：Wails 从 ico 里挑最接近系统小图标尺寸（100%–200% 缩放下是 16、20、24、32）的那张；给它一张大 PNG 时由 Windows 现场缩小，很糊。
- **`build/windows/info.json` 是手工改过的**：改成了 0409 字符串表，并加上固定的产品版本。运行 `wails3 task common:update:build-assets` 会按模板覆盖它，覆盖后要改回来。
- **Task 并行执行依赖任务**：一个失败会取消其他任务，别的任务可能报出误导性的错误（比如 "npm isn't installed"）。要找第一个真正的错误。
- **读取 JSON 配置时先用默认值填好结构体再解码**（见 `store.SettingsStore.Load`）：否则后来新增的 bool 字段会被读成 false。
- **`core.Dial`（Xray，M3）是异步的**：失败通过 `session.TrackedConnectionError` 事件送达；目标地址非法时会 panic，要先校验。
- **2026-04 起，打开 .rdp 文件每次都会弹安全对话框**：所以只用 `mstsc /v:` 直连模式（用户已拍板）。剪贴板等设置沿用 mstsc 的全局 `Default.rdp`。
- **Windows 上计时可能读到 0**：单调时钟的精度比本机回环往返还粗，测试里不要断言耗时大于 0。
- **测试在被测包之外，`go vet` 要求跨包的结构体字面量写字段名**：`model.Target{Host: "pc.example.com", Port: 3389}`，不能写成 `model.Target{"pc.example.com", 3389}`，CI 的 `go vet ./...` 会报错。
- **本机跑不了 `-race`**：没有 gcc。
- **Windows 上 `Wait` 之后再 `os.Process.Kill`，返回的是 `EINVAL`，不是 `ErrProcessDone`**：`mstsc.Process` 因此改用自己持有的句柄调 `TerminateProcess`。
- **`windows.NewCallback` 创建的回调释放不掉，数量也有上限**：只能在包级变量里创建一次（见 `winx` 的 `enumCallback`、`winEventCallback`），不要在函数里每次新建。
- **跨进程的 `SetWindowText` 不给对方发 `WM_SETTEXT`**（2026-10-06 实验）：它只改 Windows 存的文字，对方的窗口过程收不到，盯着对方进程的 WinEvent 钩子也收不到名称变化事件。要像对方自己改标题那样，就直接 `SendMessage(WM_SETTEXT)`（`winx.SetTitle`）。读标题用 `InternalGetWindowText`（`winx.Title`）：`GetWindowText` 读别的进程的窗口时，没有标题栏（比如全屏）就返回空。
- **out-of-context 的 WinEvent 钩子要求装钩子的线程有消息循环**：事件在这个线程等消息时送到。`winx.WatchWindows` 每次开一个锁定的线程，`stop` 用 `PostThreadMessage(WM_QUIT)` 结束它。测试里等标题变化也用 `WatchWindows`，不要轮询。
- **用替身进程的测试包必须有 `TestMain`，并且第一行调用 `testutil.RunHelper()`**：否则子进程会把整套测试再跑一遍。`HelperCommand` 带了 `-test.run=^$` 作为兜底。
- **会话 / 隧道的回调里不能阻塞**：`tunnel.Reporter` 的方法不能等任何东西，因为 `Close` 要等它们返回；`Manager` 的 `Changed` / `Log` 回调里不能调 `Quit`。
- **Xray 的几个行为**（engine 已经处理，改动 engine 时要记得）：
  - 日志处理器是进程级全局的：每次 `core.New` 都会注册 Xray 自己的那个，所以 engine 在 `core.New` 之后才注册桥接。
  - 第一个加入的 outbound 会成为默认出站：所以 base 配置里先放 blackhole。
  - `RemoveHandler` 只是把 outbound 从表里删掉，不会关闭它：要自己调 `Close`。
  - `Dispatch` 遇到非法目标会 panic：拨号前必须先校验地址。
- **测试要看引擎日志时，先启动 `xraytest` 代理，再启动引擎**：创建 Xray 实例会顶掉日志桥接。
- **Xray 26.x 的配置变化**（engine 已经处理，升级 Xray 时要跑 engine 测试）：
  - `allowInsecure` 在 2026-06-01 之后直接报错，自签名证书只能用 `pinnedPeerCertSha256`（`ProxyOptions.PinnedCerts`）。链接里要求跳过验证的，导入时给提示，不照做。
  - HTTP/2（`h2`、`http`）和 QUIC 传输、旧 XTLS 已删除，链接导入时直接拒绝。
  - mKCP 的伪装（headerType）和 seed 挪到了 `finalmask`：伪装头在前，`mkcp-aes128gcm`（有 seed）或 `mkcp-original`（没有）在后。
  - Hysteria2 的 TLS 要 ALPN `h3`；拥塞、带宽、端口跳跃挪到了 `finalmask.quicParams`。
- **Xray 的配置代码碰到某些畸形值会直接 panic**（例如截断的 VLESS Encryption 字符串，切片越界），而不是返回错误：model 校验先挡住已知的，`engine.build` / `add` 里 `recoverConfig` 兜底成 `proxy.config`。
- **更糟的是有些值 Xray 照收不误，连接时才在它自己的 goroutine 里 panic**（recover 不到，整个程序崩掉）：finalmask 的 fragment 长度为负、noise 尺寸为负、XHTTP 的各种长度为负等。所以 `model/masks.go` 规定这些 JSON 里不许出现负数，分享链接和表单里的 finalmask 只认伪装头、mKCP 混淆、Salamander；`engine.Outbound` 生成前一律先过完整校验，任何来源（包括手改的文件）的代理都到不了 Xray。新增可直接透传给 Xray 的 JSON 字段时，要照此处理。
- **REALITY 服务端第一次有人连时，如果对目标站点的探测还没做完，会整整睡 5 秒**（上游行为，真实服务器启动时就探测了）。`xraytest` 的目标站点握手后就关连接，探测才能马上结束；同一进程里第一个 REALITY 用例仍要等这 5 秒。
- **Fluent 对话框打开时如果里面没有可聚焦的东西（比如只有转圈），焦点陷阱（tabster modalizer）就不会激活**，之后每次在对话框里获得焦点都会被拉到对话框外面：下拉框一开就关，输入框只能打进一个字。要么等数据读完再渲染对话框（`ProxyDialog` 的做法），要么打开时就让某个输入框 autoFocus。
- **Fluent 对话框内容区是可滚动的 flex 列时，子元素会被压扁重叠**：给子元素 `flexShrink: 0`（`ProxyDialog` 的 `content` 样式）。
- **Fluent 对话框打开时聚焦它里面的第一个可聚焦元素**：只读（字段全禁用）的对话框里，焦点会落到内容区剩下的某个链接上，把内容滚过去，顶部的说明就看不见了。所以 `ProxyDialog` 锁住时不显示「显示高级设置」。
- **Fluent `Dropdown` 默认至少 250px 宽，按钮里是裸文本（长文字不会出省略号），展开的列表和按钮一样宽**：`ProxyPicker`（连接列表）用 `button` 槽放一个带省略号的 span；要让列表按内容放宽，关掉 `matchTargetSize`，同时把 `autoSize` 限成 `"height"`，否则 Fluent 用内联 `max-width` 盖掉样式里的上限。
- **`Caption1`、`Body1` 等是行内的 `span`，`maxWidth` 加省略号对它们不起作用**：要截断时加 `display: "inline-block"`（或放进 flex 容器）。
- **浏览器预览里复制不到剪贴板**：网页剪贴板被拒，Wails 的 `Clipboard.SetText` 在 server 模式下是空操作却返回成功。桌面版两者都能用；要核对复制的内容，直接调服务（如 `ProxyService.ShareLink`）。
- **测「代理不通」的用例要等约 1.5 秒**：这是 Xray 内部的重试，不是我们的超时。
- **PowerShell 命令开头那行 PATH 设置里有 `C:\Program Files`，同一条命令里再写 `Remove-Item` 会被工具的安全检查拦下**：删除操作单独一条命令执行，或者改用 Bash。
- **Wails 的单实例判断在 `application.New` 里**：第二个实例在那里直接 `os.Exit`。所以打开日志文件、载入数据、动凭据都必须放在 `application.New` 之后，服务用 `app.RegisterService` 注册（见 `app.Run`）。
- **凭据管理器的域类型凭据只接受它认识的目标名前缀**（如 `TERMSRV/`）：`foo/bar` 这类名字会报「参数错误」。所以测试凭据也放在 `TERMSRV/` 下，用保留的 `.invalid` 主机名。
- **Xray 的失败原因只有文本**：它的重试把原始错误格式化成文字。engine 的 `classify` 按文本归类，升级 Xray 时要跑 engine 测试确认文本没变。
- **新增错误码、通知代码、会话日志键时，要在两份语言文件里加翻译**：`errors.<code>`、`notices.<code>`、`log.<key>`。`internal/app` 的测试会检查。用 `errcode.Wrap` 的代码要 `errcode.Declare`。
- **Wails 运行时默认拦下从资源管理器拖进窗口的文件**（`EnableFileDrop` 为 false 时把光标设成禁止，网页收不到 drop）：所以 .rdp 导入用隐藏的 `<input type="file">`，桌面版和浏览器预览都能用。预览里测导入时，用脚本给这个 input 塞一个 `File` 再派发 `change`。
- **`LsaIso.exe` 在跑不等于 Credential Guard 在跑**：Key Guard（保护 Windows Hello 密钥）也会启动它，用户的 Win10 专业版上就是这样。判断 Credential Guard 要看 WMI `Win32_DeviceGuard.SecurityServicesRunning` 里有没有 1（`winx.CredentialGuardRunning`）。
- **在 Go 里用 COM（WMI 经 go-ole、ShellExecute）**：都经过 `winx.withCOM`：锁住线程；`CoInitializeEx` 返回 S_FALSE（这个线程已经按同样方式初始化过）也要配对 `CoUninitialize`，返回 RPC_E_CHANGED_MODE（已按别的方式初始化）则照用、不反初始化。
- **WMI 可能很久不回答（仓库损坏时）**：所以诊断报告不等 Credential Guard，它在后台只查一次，查完发 `diag:changed` 带着新报告；不设超时。
- **Wails 运行时的 debug 日志带着服务调用的参数（也就是密码）**：`Binding call complete`、`Runtime call` 都会记 `args`。`logging.SlogHandler` 因此不收 Wails 的 debug、info 记录（任何级别），也不收服务方法返回的预期错误，载荷属性写成 `<omitted>`。改它时别放开。
- **数据全部在 exe 所在的文件夹（用户要求，2026-10-06）**：`data\`（设置、代理、连接、WebView2 缓存）和 `logs\` 并列在 exe 旁边，`logs` 不在 `data` 里面。不要往 `%APPDATA%`、`%LOCALAPPDATA%` 写任何东西；WebView2 的 `WebviewUserDataPath` 也指向 `data\WebView2`，不用 Wails 的默认值（`%APPDATA%\<exe 名>`）。凭据管理器里的密码和 mstsc 的注册表记忆不得不在外面，因为 mstsc 只从那里读。
- **程序本身绝不提权运行**：标准用户借管理员账户过 UAC 时，提权的进程是那个管理员，DPAPI 加密的密码和凭据管理器里的密码都会落到错的账户下。只有 `--prepare-folders` 这个助手提权，它只建 `data`、`logs` 并给原账户加「修改」权限，然后退出。
- **UAC 提权的进程不继承调用者的环境变量，也看不到调用者映射的网络驱动器**：所以助手自己从 exe 位置算出文件夹，不读 `RDP_OVER_PROXY_HOME`；设了这个变量、或者 exe 在网络共享上（`winx.OnLocalDisk` 为假）时不提议提权，直接报错。
- **提权进程在 Program Files 里建的文件夹继承「Users：只读」**：只建文件夹不够，要 `winx.AllowModify` 给原账户加可继承的「修改」权限，以后普通权限启动才写得进去。`AllowModify` 只接受用户账户的 SID，拒绝 Everyone、Users 这类组：提权进程照命令行办事，不能被人借去给所有人开写权限。
- **Go 的 `syscall.Errno.Error()` 向 Windows 要的是英文说明**：中文的提示里要用 `winx.ErrorText(err, "zh-CN")`，系统没有该语言的文本时它退回英文。
- **读到语言设置之前弹的框要中英双语**（`i18n.Both`，系统语言在前）：准备数据文件夹失败时 `settings.json` 可能根本还没法存在。
