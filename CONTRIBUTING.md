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
go test ./internal/...                       # Go 单元测试
npm --prefix frontend run typecheck          # 前端类型检查
npm --prefix frontend test                   # 前端单元测试
wails3 generate bindings -clean=true -ts -i  # 修改 Go 服务后重新生成前端绑定
```

开发和测试时可以设置环境变量 `RDP_OVER_PROXY_HOME=<某个目录>`，程序的全部数据都会写到这个目录下，不会碰你自己的设置。

### 约定

- **事件驱动**：处理时序时等真正的事件（Listen 返回、进程退出、上游报错），不要写 sleep、固定次数重试或随意设定的超时。确实没有事件可等的地方，要在代码注释里说明原因。
- **双语**：界面文案只能放在 `frontend/src/locales/zh-CN.json` 和 `en.json`，两边的 key 必须一致，有测试检查。文档类文件需要同时更新中英两部分。
- **测试纪律**：测试只能使用自己的端口（隧道用 port 0），也只能结束自己启动的进程。测试数据使用 `example.com` 和 `192.0.2.x`，不要放真实的主机名或凭据。
- **版本号**：请不要在 PR 里修改版本号，由维护者统一决定。
- **Wails 版本**：Go module `github.com/wailsapp/wails/v3` 和 npm 包 `@wailsio/runtime` 必须是同一个版本，升级时一起改。

### 提交 PR 之前

- `go test ./internal/...`、`npm --prefix frontend run typecheck` 和 `npm --prefix frontend test` 都要通过。
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
go test ./internal/...                       # Go unit tests
npm --prefix frontend run typecheck          # frontend type check
npm --prefix frontend test                   # frontend unit tests
wails3 generate bindings -clean=true -ts -i  # regenerate the frontend bindings after changing Go services
```

While developing or testing, set `RDP_OVER_PROXY_HOME=<some folder>` and all of the app's data goes there instead of your own settings.

### Conventions

- **Event-driven timing.** Wait for the real event (Listen returned, a process exited, the upstream reported an error). Don't add sleeps, fixed retry counts or arbitrary timeouts. Where there is truly nothing to wait for, explain why in a comment.
- **Two languages.** UI text lives only in `frontend/src/locales/zh-CN.json` and `en.json`, and both must have the same keys (a test checks this). Update both language sections of documentation files.
- **Test hygiene.** Tests use their own ports (tunnels listen on port 0) and only stop processes they started. Use `example.com` and `192.0.2.x` in test data — never real host names or credentials.
- **Versions.** Please don't change version numbers in pull requests; the maintainer decides them.
- **Wails versions.** The Go module `github.com/wailsapp/wails/v3` and the npm package `@wailsio/runtime` must stay on the same version and be upgraded together.

### Before opening a pull request

- Make sure `go test ./internal/...`, `npm --prefix frontend run typecheck` and `npm --prefix frontend test` all pass.
- For UI changes, attach screenshots of both the Chinese and the English UI.
