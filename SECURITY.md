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
- **程序的文件**：全部在程序所在的文件夹里（`data`、`logs`），不写用户目录。能读这个文件夹的人都能看到其中的连接和代理设置（主机名、用户名等），密钥是加密的。在多人共用的电脑上，请把程序放在只有你能访问的文件夹里。
- **管理员权限**：程序本身从不以管理员权限运行。放在只有管理员能写入的文件夹（如 Program Files）时，只有一个小步骤经 UAC 提权：建好 `data`、`logs` 两个文件夹并允许你的账户写入，然后立即退出。
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
- **The app's files** are all in the folder it runs from (`data`, `logs`), never in your user profile. Whoever can read that folder can see the connection and proxy settings in it (host names, user names); the secrets are encrypted. On a shared PC, keep the app in a folder only you can open.
- **Administrator rights**: the app itself never runs with them. In a folder only administrators can write to (such as Program Files), one small step goes through UAC: it creates the `data` and `logs` folders, lets your account write to them, and exits at once.
- **Tunnels** listen only on loopback addresses (127.x.y.z), and only while a Remote Desktop session is running.
- **No telemetry.** The app doesn't collect or upload any usage data.
