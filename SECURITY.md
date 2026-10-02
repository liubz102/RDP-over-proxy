# 安全策略 / Security Policy

[中文](#中文) | [English](#english)

## 中文

### 报告漏洞

请**不要**在公开的 Issue 里报告安全漏洞。请在本仓库的 **Security** 页面点击「Report a vulnerability」，通过 GitHub 的私密漏洞报告提交。

报告中请尽量写清楚：
- 受影响的版本；
- 复现步骤；
- 可能造成的影响。

我们会尽快确认并处理；修复发布之前，请不要公开漏洞细节。

### 本程序如何处理敏感数据

- **远程桌面密码**：只有勾选「记住密码」时才会保存，并且只存在 Windows 凭据管理器里，不会写入程序的配置文件。
- **代理密码等密钥**：用 Windows DPAPI 按当前用户加密后，再写入配置文件。
- **隧道监听地址**：只监听本机回环地址（127.x.y.z），并且只在远程桌面会话期间打开。
- **数据收集**：程序不收集、也不上传任何使用数据。

---

## English

### Reporting a vulnerability

Please **don't** report security vulnerabilities in public issues. Use GitHub's private vulnerability reporting: open this repository's **Security** tab and choose "Report a vulnerability".

Please include as much as you can of:
- the affected version;
- steps to reproduce;
- the possible impact.

We'll confirm and work on it as soon as we can. Please keep the details private until a fix is released.

### How the app handles sensitive data

- **Remote Desktop passwords** are stored only when you choose "Remember password", and only in Windows Credential Manager — never in the app's own files.
- **Proxy passwords and other secrets** are encrypted with Windows DPAPI for the current user before they're written to the app's files.
- **Tunnels** listen only on loopback addresses (127.x.y.z), and only while a Remote Desktop session is running.
- **No telemetry.** The app doesn't collect or upload any usage data.
