# 开发进度

最后更新：2026-10-02

## 里程碑

| M | 内容 | 状态 | 用户验收 |
|---|---|---|---|
| M0 | 起步：环境、脚手架、仓库文件、界面外壳 | ✅ 完成，待用户验收 | — |
| M1 | 领域核心：model、loopback、rdpfile 解析、mstsc 参数组装、X.224 编解码、状态机、假 RDP 服务端 | 未开始 | |
| M2 | 运行时（直连代理）：tunnel、启动器、会话 actor 和 Manager、清理 | 未开始 | |
| M3 | Xray 引擎 + SOCKS / HTTP | 未开始 | |
| M4 | 存储与服务：store、DPAPI、TERMSRV 凭据、UsernameHint、错误码、全部服务与事件 | 未开始 | |
| M5 | 界面 v1，与原型功能对等（用户实连验证） | 未开始 | |
| M6 | v2ray 系协议：分享链接、各协议表单、自定义 JSON、测速 | 未开始 | |
| M7 | 导入与诊断：.rdp 导入、「调整默认设置」、环境报告、测试面板 | 未开始 | |
| M8 | 打磨：视觉、图标、空状态、日志查看、关于与第三方声明 | 未开始 | |
| M9 | 发布工程：notices 工具、完整 CI、release 工作流、NSIS 安装包、README 定稿 | 未开始 | |

## M0 完成情况

**环境**
- 用 winget 安装了 Go 1.27.0。
- 安装了 wails3 v3.0.0-beta.27。

**原型迁移**
- 原型整体移入 `legacy/`，包括 `bin/rdp-tunnel.exe` 和 `targets/*.rdp`，都已被 `.gitignore` 排除。

**项目骨架**
- 用 Wails v3 的 React-TS 模板生成，删掉了非 Windows 的构建文件。
- 填入正式的产品信息，版本 0.1.0。
- `info.json` 改为 en-US 字符串表，资源管理器能正确显示版本信息。

**Go 端**
- `model`：Settings 及其 Normalize / Repair / Validate。
- `store`：原子写 JSON；设置的加载、损坏恢复、缺字段补默认值；`RDP_OVER_PROXY_HOME` 数据隔离。
- `i18n`：托盘文案、系统语言检测。
- `winx`：WebView2 检测、错误框、系统深色检测。
- `api`：`SettingsService` 和 `settings:changed` 事件。
- `app`：窗口、托盘、单实例、关闭缩到托盘、启动错误弹框；server 版单独处理。

**前端**
- React 19 + Fluent UI v9 + i18next + zustand。
- 首次启动的双语语言选择。
- 侧栏外壳：选中项带强调色竖条。
- 设置页：语言、主题、关闭行为、关于。
- 连接页和代理页目前是占位的空状态。

**仓库文件**
- `.gitignore`、`.gitattributes`、`.editorconfig`、`LICENSE`（MIT）。
- 中英 README、`CLAUDE.md`、`docs/*`、`CONTRIBUTING.md`、`SECURITY.md`、`.github/*`（CI、dependabot、Issue/PR 模板）。

## 验证记录

| 日期 | 范围 | 结果 |
|---|---|---|
| 2026-10-02 | `go vet`（桌面版、`-tags server`） | 通过 |
| 2026-10-02 | `go test ./internal/...` | 通过（model、store、i18n） |
| 2026-10-02 | 前端 `tsc` / `vitest` | 无错误 / 2 项通过 |
| 2026-10-02 | `wails3 build` | 成功，exe 约 11 MB，版本信息 RDP over Proxy 0.1.0 |
| 2026-10-02 | 浏览器预览（server 模式） | 通过，见下 |
| 2026-10-02 | 原生自测（隔离数据目录） | 通过，见下 |
| — | 用户在真实环境查看界面 | 待做 |

浏览器预览检查的内容：
- 首次启动语言选择：切换后中英文先后顺序跟着变。
- 进入主界面；在设置里切换语言、切换深色主题。
- 关于信息正确；刷新后设置仍然保留。

原生自测检查的内容：
- 单实例：第二个实例启动后自己退出，第一个继续运行。
- 关闭窗口后进程仍在、窗口已隐藏。

## 下一步（M1 领域核心）

1. **`model`**：Proxy、Profile 的数据结构和校验。
   - Profile 字段：target、proxyId、loopback、username、rememberPassword、display、admin。
2. **`loopback`**：由连接 ID 派生 `127.(1-254).(1-254).(1-254)`，带查重。
3. **`rdpfile`**：只读解析 .rdp（UTF-16 / UTF-8），用于导入；判断 `Default.rdp` 会不会走 RD 网关。
4. **`mstsc`**：用纯函数组装启动参数：`/v /f /w /h /multimon /span /admin`。
5. **`probe`**：X.224 的编码和解码，覆盖 6 种失败码和截断报文。
6. **`session`**：状态机，用纯函数 reducer 实现。
7. **`testutil`**：假 RDP 服务端，用于后续的端到端测试。

## 决策记录

| 日期 | 决定 | 原因 | 放弃的方案 |
|---|---|---|---|
| 2026-10-02 | 项目名 RDP-over-proxy，初始版本 0.1.0 | 用户指定 | RdpTunnel、DeskTunnel |
| 2026-10-02 | Go + Wails v3（锁定 beta.27）+ React/TS + Fluent UI v9 | 编译成单个 exe，可内嵌 Xray；前端用用户熟悉的 React/TS；v3 自带托盘 | Wails v2（没有托盘）、C# WPF + Go 核心（两套工具链） |
| 2026-10-02 | 代理单独管理，多个连接引用同一个 | 换节点只需改一处 | 每个连接各自填写 |
| 2026-10-02 | 只用直连模式 `mstsc /v:` | 2026-04 补丁后打开 .rdp 每次都弹框；用户认为两种模式难以理解 | RDP 文件模式 + 自动选择 |
| 2026-10-02 | 内嵌 Xray-core 作为库，不随附 exe | 许可证清晰，单个 exe | 随附改名的 v2ray.exe |
| 2026-10-02 | 分享链接自己解析 | libXray 拒绝 v2rayN 旧式的 vmess 链接，并且绑定的是 Xray 预发布版 | 依赖 libXray/share |
| 2026-10-02 | 只保留 Windows 构建文件 | 程序只能在 Windows 上运行 | 保留模板里的全部平台 |
| 2026-10-02 | 中英双语，首次启动选择语言 | 用户要求 | 自动跟随系统语言 |
| 2026-10-02 | MIT 许可证 | 与 MPL-2.0 的依赖兼容 | GPL（如果用 sing-box 就只能选它） |
| 2026-10-02 | 预览版和自测使用独立的数据目录 | 不覆盖用户的设置，不用掉首次启动体验 | 共用真实数据目录 |

## 已知问题

- **应用图标**：仍是 Wails 默认图标，M8 时换。
- **前端体积**：bundle 约 646 KB，是 Fluent UI 全量引入造成的，暂不处理。
- **标题栏颜色**：只跟随系统主题，不随应用内的主题设置变化。Wails 运行时能否修改标题栏主题，还待查。
- **托盘右键菜单**：在 Wails v3 beta 上可能弹不出来（#6161），目前用左键打开窗口代替。

## 待用户回答的问题

- （暂无）

## 本机环境准备状态

- Go 1.27.0：已装，`C:\Program Files\Go`。
- wails3 v3.0.0-beta.27：已装，`%USERPROFILE%\go\bin`。
- Node 24.14.0 / npm 11.9.0：已装。
- NSIS：未装，M9 打包时再装。
