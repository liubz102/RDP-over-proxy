# 参与贡献 / Contributing

[中文](#中文) | [English](#english)

## 中文

感谢你愿意参与！这个项目只支持 Windows：它要调用 mstsc、Windows 凭据管理器和 DPAPI。

### 开发环境

- Windows 10 或 11（64 位）
- Go 1.27
- Node.js 24
- Wails v3 命令行工具：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27
```

### 常用命令

```powershell
wails3 dev                                   # 开发模式（热重载）
wails3 build                                 # 构建 bin\RDP-over-proxy.exe
go test ./...                                # Go 单元测试（都在 tests/ 下）
npm --prefix frontend run typecheck          # 前端类型检查
npm --prefix frontend test                   # 前端单元测试
wails3 generate bindings -clean=true -ts -i  # 修改 Go 服务后重新生成前端绑定
```

程序把数据放在 exe 旁边的 `data` 和 `logs` 文件夹里。开发和测试时可以设置环境变量 `RDP_OVER_PROXY_HOME=<某个目录>` 代替 exe 所在的文件夹（数据写到 `<某个目录>\data` 和 `<某个目录>\logs`），或者把 exe 复制到一个临时文件夹里运行，都不会碰你自己的数据。每个数据文件夹只运行一个实例，所以这样启动的程序不会和你正在用的那个互相干扰。注意 Windows 凭据管理器和 mstsc 的注册表不在这些文件夹里，自测时不要保存远程桌面密码。

### 约定

- **事件驱动**：处理时序时等真正的事件（Listen 返回、进程退出、上游报错），不要写 sleep、固定次数重试或随意设定的超时。确实没有事件可等的地方，要在代码注释里说明原因。
- **双语**：界面文案只能放在 `frontend/src/locales/zh-CN.json` 和 `en.json`，两边的 key 必须一致，有测试检查。Go 侧新增错误码、通知或会话日志时，要在两份文件的 `errors`、`notices`、`log` 下加翻译，也有测试检查。文档类文件需要同时更新中英两部分。
- **测试和代码分开**：Go 测试放在 `tests/<包名>/`（例如 `tests/session` 测 `internal/session`），写成外部测试包 `<包名>_test`，只用包导出的 API；前端测试放在 `frontend/tests/`，目录结构和 `frontend/src` 对应。不要把测试文件放进 `internal/` 或 `frontend/src`。
- **测试纪律**：测试只能使用自己的端口（隧道用 port 0），也只能结束自己启动的进程。测试数据使用 `example.com` 和 `192.0.2.x`，不要放真实的主机名或凭据。测试写凭据管理器时只用 `TERMSRV/rdp-over-proxy-test-<随机>.invalid`，写注册表时只用 `HKCU\Software\RDP-over-proxy-test`，并在结束时清掉。
- **版本号**：请不要在 PR 里修改版本号，由维护者统一决定。
- **Wails 版本**：Go module `github.com/wailsapp/wails/v3` 和 npm 包 `@wailsio/runtime` 必须是同一个版本，升级时一起改。

### 提交 PR 之前

- `go vet ./...`、`go test ./...`、`npm --prefix frontend run typecheck` 和 `npm --prefix frontend test` 都要通过。
- 涉及界面的改动，请附上中文和英文界面的截图。

---

## English

Thanks for helping! This project is Windows-only: it drives mstsc, Windows Credential Manager and DPAPI.

### Setup

- Windows 10 or 11 (64-bit)
- Go 1.27
- Node.js 24
- The Wails v3 CLI:

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27
```

### Common commands

```powershell
wails3 dev                                   # development mode with hot reload
wails3 build                                 # builds bin\RDP-over-proxy.exe
go test ./...                                # Go unit tests (all under tests/)
npm --prefix frontend run typecheck          # frontend type check
npm --prefix frontend test                   # frontend unit tests
wails3 generate bindings -clean=true -ts -i  # regenerate the frontend bindings after changing Go services
```

The app keeps its data in the `data` and `logs` folders next to the exe. While developing or testing, set `RDP_OVER_PROXY_HOME=<some folder>` to stand in for the exe's folder (the data goes to `<some folder>\data` and `<some folder>\logs`), or run a copy of the exe from a temporary folder; either way your own data is left alone. Each data folder runs one instance, so such a run goes alongside the copy you use day to day. Windows Credential Manager and mstsc's registry settings are not in those folders, so don't save Remote Desktop passwords in such runs.

### Conventions

- **Event-driven timing.** Wait for the real event (Listen returned, a process exited, the upstream reported an error). Don't add sleeps, fixed retry counts or arbitrary timeouts. Where there is truly nothing to wait for, explain why in a comment.
- **Two languages.** UI text lives only in `frontend/src/locales/zh-CN.json` and `en.json`, and both must have the same keys (a test checks this). A new error code, notice or session log line on the Go side needs a translation under `errors`, `notices` or `log` in both files (a test checks this too). Update both language sections of documentation files.
- **Tests live apart from the code.** Go tests go in `tests/<package>/` (for example `tests/session` tests `internal/session`) as an external test package `<package>_test` that uses only what the package exports; frontend tests go in `frontend/tests/`, mirroring `frontend/src`. Don't put test files in `internal/` or `frontend/src`.
- **Test hygiene.** Tests use their own ports (tunnels listen on port 0) and only stop processes they started. Use `example.com` and `192.0.2.x` in test data — never real host names or credentials. Tests that write to Credential Manager use only `TERMSRV/rdp-over-proxy-test-<random>.invalid`, tests that write to the registry use only `HKCU\Software\RDP-over-proxy-test`, and both clean up after themselves.
- **Versions.** Please don't change version numbers in pull requests; the maintainer decides them.
- **Wails versions.** The Go module `github.com/wailsapp/wails/v3` and the npm package `@wailsio/runtime` must stay on the same version and be upgraded together.

### Before opening a pull request

- Make sure `go vet ./...`, `go test ./...`, `npm --prefix frontend run typecheck` and `npm --prefix frontend test` all pass.
- For UI changes, attach screenshots of both the Chinese and the English UI.
