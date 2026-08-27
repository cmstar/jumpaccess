# JumpAccess

JumpAccess 是一个面向 JumpServer 的独立访问工具项目。首个交付物是 Go 编写的命令行程序 `jumpctl`；项目同时保留共享核心能力，便于未来在需求明确后增加桌面入口，例如采用 Wails 的 GUI。

项目当前处于首个可用版本的开发阶段。配置、Profile、Alias、OAuth Token 生命周期、JumpServer 连接准备 API 和直接 SSH 已经实现；ProxyCommand 仍在开发。实际状态以代码、测试和发布说明为准。

## 项目目标

- 不依赖 JumpServer 桌面 Client，独立完成浏览器 OAuth 登录和 Token 生命周期管理。
- 以 SSH 客户端方式直接连接远程 Asset。
- 作为通用 SSH `ProxyCommand` 被兼容的终端或 SSH 客户端调用，不与某个具体终端产品耦合。
- 通过 TOML 管理多个 JumpServer Profile 以及 Asset 别名等非敏感配置。
- 支持 Windows 和 macOS，并将认证秘密交给操作系统安全凭据存储。

首个兼容性参考基线是 JumpServer Client `v4.1.6` 所使用的协议和接口。它是分析参考，不表示本项目依赖该桌面 Client 才能运行。

## 开发环境与当前状态

项目已经建立 Go 工程、`jumpctl` 入口、TOML 配置、Profile、Alias、浏览器 OAuth 登录、原生凭据存储、并发安全的 Token 刷新、JumpServer 连接准备协议，以及带主机密钥校验的交互式 SSH 客户端。ProxyCommand 尚在后续阶段实现。

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
jumpctl auth login [--profile <name>]
jumpctl auth status [--profile <name>]
jumpctl auth refresh [--profile <name>]
jumpctl auth logout [--profile <name>]
jumpctl ssh <target> [--profile <name>] [--organization <org>] [--account <account>]
```

## 安全说明

不要把账号密码、Access Token、Refresh Token、Cookie、私钥或其他真实凭据写入仓库。真实 JumpServer 账号只用于开发者本机的手工 smoke test；常规自动化测试应使用模拟服务和脱敏 fixture。

Windows 使用 Credential Manager 保存 OAuth 凭据。macOS 使用 Keychain；包含 Keychain 后端的 macOS 正式构建需要启用 CGO 并链接系统 Security framework。

首次直接连接某个 JumpServer SSH gateway 时，`jumpctl ssh` 会显示 SHA-256 主机密钥指纹并要求明确确认；信任记录保存在同一 JumpAccess 应用目录的 `known_hosts`。主机密钥变化不会自动接受。

## 文档

- [Agent 工作入口](AGENTS.md)
- [架构说明](docs/architecture.md)
- [业务说明](docs/domain.md)
- [开发说明](docs/development.md)

## 发布

当前尚未建立发布和安装流程。形成可交付的 Windows、macOS 构建产物后，应补充受支持的平台、安装方式、版本策略和校验方法。
