# JumpAccess

JumpAccess 是一个面向 JumpServer 的独立访问工具项目。首个交付物是 Go 编写的命令行程序 `jumpctl`；项目同时保留共享核心能力，便于未来在需求明确后增加桌面入口，例如采用 Wails 的 GUI。

项目当前处于首个可用版本的开发阶段。配置、Profile、Alias、OAuth Token 生命周期、JumpServer 连接准备 API、直接 SSH 和通用 ProxyCommand 已经实现。真实 JumpServer 已确认接受官方 `jms://auth/callback` 而拒绝未登记的 loopback Redirect URI；完整 Token 交换、SSH 链路与 macOS 原生环境仍需 smoke test。实际状态以代码、测试和发布说明为准。

## 项目目标

- 不依赖 JumpServer 桌面 Client，独立完成浏览器 OAuth 登录和 Token 生命周期管理。
- 以 SSH 客户端方式直接连接远程 Asset。
- 作为通用 SSH `ProxyCommand` 被兼容的终端或 SSH 客户端调用，不与某个具体终端产品耦合。
- 通过 TOML 管理多个 JumpServer Profile 以及 Asset 别名等非敏感配置。
- 支持 Windows 和 macOS，并将认证秘密交给操作系统安全凭据存储。

首个兼容性参考基线是 JumpServer Client `v4.1.6` 所使用的协议和接口。它是分析参考，不表示本项目依赖该桌面 Client 才能运行。

## 开发环境与当前状态

项目已经建立 Go 工程、`jumpctl` 入口、TOML 配置、Profile、Alias、浏览器 OAuth 登录、原生凭据存储、并发安全的 Token 刷新、JumpServer 连接准备协议、直接 SSH 客户端，以及基于本地 SSH server façade 的通用 ProxyCommand。

当前已经确定的 Go module 路径为：

```text
github.com/cmstar/jumpaccess
```

环境要求：Go 1.24 或更高版本。

## 本地运行、测试与构建

```powershell
go run ./cmd/jumpctl --help
go test ./...
go vet ./...
go build -trimpath ./cmd/jumpctl
```

当前已经实现的配置入口包括：

```text
jumpctl version
jumpctl config path
jumpctl config edit
jumpctl config validate
jumpctl profile add <name> --url <site>
jumpctl profile list
jumpctl profile use <name>
jumpctl alias set <name> --asset <asset> [--account <account>] [--organization <org>]
jumpctl alias list
jumpctl auth login [--profile <name>] [--manual]
jumpctl auth status [--profile <name>]
jumpctl auth refresh [--profile <name>]
jumpctl auth logout [--profile <name>]
jumpctl organization list [--profile <name>]
jumpctl asset list [--profile <name>] [--organization <org>] [--search <text>]
jumpctl account list <asset> [--profile <name>] [--organization <org>]
jumpctl ssh <target> [--profile <name>] [--organization <org>] [--account <account>]
jumpctl proxy <target> [--profile <name>] [--organization <org>] [--account <account>]
```

通用 OpenSSH 配置示例（`web` 可以是已绑定唯一 Asset 和 Account 的 Alias）：

```sshconfig
Host production-web
    HostName web
    User jumpaccess
    ProxyCommand jumpctl proxy %h
```

当前开发版本的 `auth login` 默认使用手工回调：浏览器完成授权后，不要点击确认页的“确认”，而是复制页面中的 `jms://` 链接或浏览器地址栏的完整确认页 URL，粘贴到等待中的终端。`--manual` 可显式固定这一行为。正式发布后计划注册 `jms` 私有协议并默认自动接收回调，但永久保留 `--manual`，供官方客户端仍占用协议或系统不允许注册协议时使用。

`proxy` 模式不打开浏览器，也不提示选择 Account。缺少登录、Refresh Token 失效、目标或 Account 不唯一、上游主机尚未信任时，进程会在 SSH banner 之前失败，只向 stderr 写入可操作错误并返回非零状态。先运行 `jumpctl auth login` 完成授权；未知上游 gateway 需要先用 `jumpctl ssh` 进行一次人工指纹确认。

## 安全说明

不要把账号密码、Access Token、Refresh Token、Cookie、私钥或其他真实凭据写入仓库。真实 JumpServer 账号只用于开发者本机的手工 smoke test；常规自动化测试应使用模拟服务和脱敏 fixture。

Windows 使用 Credential Manager 保存 OAuth 凭据。macOS 使用 Keychain；包含 Keychain 后端的 macOS 正式构建需要启用 CGO 并链接系统 Security framework。

首次直接连接某个 JumpServer SSH gateway 时，`jumpctl ssh` 会显示 SHA-256 主机密钥指纹并要求明确确认；信任记录保存在同一 JumpAccess 应用目录的 `known_hosts`。主机密钥变化不会自动接受。

ProxyCommand 存在两层独立的主机信任：外部 SSH 客户端看到的是 JumpAccess 本地 façade 的稳定 Ed25519 host key，该私钥保存在操作系统凭据存储；JumpAccess 自己仍使用上述 `known_hosts` 严格验证上游 JumpServer gateway。

## 文档

- [Agent 工作入口](AGENTS.md)
- [架构说明](docs/architecture.md)
- [业务说明](docs/domain.md)
- [开发说明](docs/development.md)
- [CLI 命令参考](docs/cli.md)

## 发布

当前尚未建立发布和安装流程。形成可交付的 Windows、macOS 构建产物后，应补充受支持的平台、安装方式、版本策略和校验方法。
