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
| `internal/app` | Wails 应用、窗口、托盘、单实例、关闭缩到托盘、启动与退出顺序。`desktop_windows.go` 和 `server.go` 用构建标签区分桌面版与 server 版；`platform_windows.go` 提供两版共用的 Windows 部件（DPAPI、凭据、mstsc） |
| `internal/api` | 暴露给前端的服务（Settings、Profile、Proxy、Session、App）和它们共用的 `Core`；视图类型、事件、错误 JSON（`MarshalError`） |
| `internal/model` | 数据结构（Settings、Proxy、Profile、Target）与校验，纯逻辑。Proxy / Profile 的校验返回 `FieldErrors`（字段 + 代码） |
| `internal/loopback` | 由 profile ID 派生 `127.a.b.c` 回环地址，冲突时顺延 |
| `internal/rdpfile` | 只读解析 .rdp：导入草稿、RD 网关判定 |
| `internal/mstsc` | mstsc 启动参数（`Args`，纯函数）；启动 / 等待 / 关闭 / 结束 / 聚焦（`launch_windows.go`）；`Servers` 注册表记忆（UsernameHint）；RD 网关检查（`DecideGateway` 纯函数 + `CheckGateway`） |
| `internal/probe` | X.224 CR/CC 编解码、线路检查 `Check` |
| `internal/route` | `Dialer` / `Provider` 接口、直连 |
| `internal/engine` | 内嵌的 Xray 实例，也是应用实际使用的 `route.Provider`：outbound 引用计数、强制 tag 派发、错误原因回传、日志桥接 |
| `internal/tunnel` | 回环入口：接受连接、经线路拨目标、双向拷贝、计数、报告 |
| `internal/session` | 会话：纯 reducer（`session.go`、`reduce.go`），外壳是 actor（`actor.go`）和 `Manager`（`manager.go`） |
| `internal/errcode` | 错误码：`New` / `Weak` / `Wrap`，`Of` 取最有用的代码；`Declare` / `All` 供翻译完整性测试 |
| `internal/secret` | DPAPI 加密（`DPAPI`）；凭据管理器里 `TERMSRV/<回环地址>` 的密码（`Vault`） |
| `internal/logging` | 日志文件（按大小轮转）、环形缓冲、脱敏、连续重复折叠、给 Wails 用的 slog 适配 |
| `internal/store` | 原子写 JSON；数据目录；`RDP_OVER_PROXY_HOME`；设置；代理和连接的文件存储（`Data`） |
| `internal/i18n` | Go 侧文案（托盘、原生对话框）、系统语言检测 |
| `internal/winx` | Win32 调用：WebView2 检测、错误框、系统深色模式 |
| `tests/<包名>` | Go 测试，每个被测包一个目录（如 `tests/session`），包名 `<包名>_test`，只用导出的 API；`tests/rdpfile/testdata` 是 .rdp 样本 |
| `tests/testutil` | 测试共用：假 RDP 服务端；替身进程（`RunHelper` / `HelperCommand`）；`FreePort`。只能被测试引用 |
| `tests/testutil/xraytest` | 测试用：进程内的 Xray SOCKS / HTTP 代理。单独成包，只有需要的测试才链接 Xray |
| `frontend/src` | `app/`（外壳、主题、首次语言选择）、`features/`、`components/`、`stores/`、`locales/` |
| `frontend/tests` | 前端测试（vitest），目录结构和 `frontend/src` 对应 |
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
  - Go 测试：`go test ./...`（测试都在 `tests/` 下）
  - Go 全量检查：`go vet ./...`。`main` 包嵌入了 `frontend/dist`，所以要先构建一次前端
  - 前端：`npm --prefix frontend run typecheck`、`npm --prefix frontend test`
- **重新生成绑定**：`wails3 generate bindings -clean=true -ts -i`
- **浏览器预览界面**
  1. 运行 `wails3 task build:server DEV=true`。
  2. 用内置浏览器工具 `preview_start` 启动 `.claude/launch.json` 里的 `preview` 配置：端口 34115，数据目录 `data\preview`（已被 git 忽略）。
  3. 页面在 `http://localhost:34115`。

  没有界面的服务可以在页面里用 `fetch("/wails/runtime")` 按方法全名调用（`object: 0`，`args: {"call-id", methodName, args}`；取消用 `object: 10`）。不经过 launch.json 手动运行时，预览版的数据放在 `%APPDATA%\RDP-over-proxy-preview`。
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
   - 测试和自测的数据用 `RDP_OVER_PROXY_HOME` 隔离（它同时让单实例 ID 按数据目录区分，自测不会和用户正在用的实例互相干扰）。
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
- **`wails3 generate icons` 要传 `-macfilename ""`**：否则会去写 `build/darwin/`，而本仓库已经删掉了这个目录。
- **`build/windows/info.json` 是手工改过的**：改成了 0409 字符串表，并加上固定的产品版本。运行 `wails3 task common:update:build-assets` 会按模板覆盖它，覆盖后要改回来。
- **Task 并行执行依赖任务**：一个失败会取消其他任务，别的任务可能报出误导性的错误（比如 "npm isn't installed"）。要找第一个真正的错误。
- **读取 JSON 配置时先用默认值填好结构体再解码**（见 `store.SettingsStore.Load`）：否则后来新增的 bool 字段会被读成 false。
- **`core.Dial`（Xray，M3）是异步的**：失败通过 `session.TrackedConnectionError` 事件送达；目标地址非法时会 panic，要先校验。
- **2026-04 起，打开 .rdp 文件每次都会弹安全对话框**：所以只用 `mstsc /v:` 直连模式（用户已拍板）。剪贴板等设置沿用 mstsc 的全局 `Default.rdp`。
- **Windows 上计时可能读到 0**：单调时钟的精度比本机回环往返还粗，测试里不要断言耗时大于 0。
- **测试在被测包之外，`go vet` 要求跨包的结构体字面量写字段名**：`model.Target{Host: "pc.example.com", Port: 3389}`，不能写成 `model.Target{"pc.example.com", 3389}`，CI 的 `go vet ./...` 会报错。
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
- **Wails 的单实例判断在 `application.New` 里**：第二个实例在那里直接 `os.Exit`。所以打开日志文件、载入数据、动凭据都必须放在 `application.New` 之后，服务用 `app.RegisterService` 注册（见 `app.Run`）。
- **凭据管理器的域类型凭据只接受它认识的目标名前缀**（如 `TERMSRV/`）：`foo/bar` 这类名字会报「参数错误」。所以测试凭据也放在 `TERMSRV/` 下，用保留的 `.invalid` 主机名。
- **Xray 的失败原因只有文本**：它的重试把原始错误格式化成文字。engine 的 `classify` 按文本归类，升级 Xray 时要跑 engine 测试确认文本没变。
- **新增错误码、通知代码、会话日志键时，要在两份语言文件里加翻译**：`errors.<code>`、`notices.<code>`、`log.<key>`。`internal/app` 的测试会检查。用 `errcode.Wrap` 的代码要 `errcode.Declare`。
- **Wails 运行时的 debug 日志带着服务调用的参数（也就是密码）**：`Binding call complete`、`Runtime call` 都会记 `args`。`logging.SlogHandler` 因此不收 Wails 的 debug、info 记录（任何级别），也不收服务方法返回的预期错误，载荷属性写成 `<omitted>`。改它时别放开。
