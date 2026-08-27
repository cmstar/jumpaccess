# JumpAccess

JumpAccess 是一个面向 JumpServer 的独立访问工具项目。首个交付物是 Go 编写的命令行程序 `jumpctl`；项目同时保留共享核心能力，便于未来在需求明确后增加桌面入口，例如采用 Wails 的 GUI。

项目当前处于工程初始化阶段。下述内容描述已经确认的产品目标，不代表相关命令或能力已经实现；实际状态以代码、测试和发布说明为准。

## 项目目标

- 不依赖 JumpServer 桌面 Client，独立完成浏览器 OAuth 登录和 Token 生命周期管理。
- 以 SSH 客户端方式直接连接远程 Asset。
- 作为通用 SSH `ProxyCommand` 被兼容的终端或 SSH 客户端调用，不与某个具体终端产品耦合。
- 通过 TOML 管理多个 JumpServer Profile 以及 Asset 别名等非敏感配置。
- 支持 Windows 和 macOS，并将认证秘密交给操作系统安全凭据存储。

首个兼容性参考基线是 JumpServer Client `v4.1.6` 所使用的协议和接口。它是分析参考，不表示本项目依赖该桌面 Client 才能运行。

## 开发环境与当前状态

应用工程、运行命令、测试命令和构建命令正在建立中。相应入口形成并经过验证后，应在这里和 [开发说明](docs/development.md) 中据实补充。

当前已经确定的 Go module 路径为：

```text
github.com/cmstar/jumpaccess
```

## 本地运行、测试与构建

应用工程尚未形成，因此当前没有可以准确列出的本地运行、测试和构建命令。首批 Go 入口和测试落地并实际执行后，应立即补充这里，避免文档提供未经验证的命令。

## 安全说明

不要把账号密码、Access Token、Refresh Token、Cookie、私钥或其他真实凭据写入仓库。真实 JumpServer 账号只用于开发者本机的手工 smoke test；常规自动化测试应使用模拟服务和脱敏 fixture。

## 文档

- [Agent 工作入口](AGENTS.md)
- [架构说明](docs/architecture.md)
- [业务说明](docs/domain.md)
- [开发说明](docs/development.md)

## 发布

当前尚未建立发布和安装流程。形成可交付的 Windows、macOS 构建产物后，应补充受支持的平台、安装方式、版本策略和校验方法。
