# 开发说明

## 当前阶段

Go 工程、配置能力、OAuth Token 生命周期和 JumpServer 连接准备协议已经建立。直接 SSH 和 ProxyCommand 仍在开发中；实际状态以测试和当前命令帮助为准。

## 技术栈、版本与开发约束

- 主要实现语言：Go 1.24 或更高版本，以根目录 `go.mod` 为准。
- Module 路径：`github.com/cmstar/jumpaccess`。
- 工程采用单个 Go module，支持多个入口适配器共享核心逻辑。
- 首个可执行入口为 `jumpctl`；未来 GUI 不得迫使 OAuth、配置、JumpServer API、目标解析或 SSH 逻辑复制一份。
- Windows 和 macOS 都是目标平台。
- JumpServer Client `v4.1.6` 是首个协议行为参考，不应成为运行时依赖。

## 目标目录边界

当前目录边界：

```text
cmd/jumpctl/        # CLI 入口适配器
internal/appdir/    # 单一应用数据根目录
internal/application/settings/ # Profile 与 Alias 修改用例
internal/application/auth/     # 登录状态、刷新与生命周期编排
internal/cli/       # CLI 参数和输出适配
internal/config/    # TOML 模型、校验和存储
internal/credential/# Windows Credential Manager 与 macOS Keychain 适配
internal/filelock/  # 多进程 Token 刷新锁
internal/jumpserver/# JumpServer REST 与 client-url 协议客户端
internal/oauth/     # OAuth Discovery、PKCE、callback 与 Token 协议
internal/systemopen/# 打开配置文件的平台适配
internal/target/    # Profile、Alias 和远程目标解析
docs/               # 长期项目知识
```

未来 GUI 可以增加独立入口，但项目继续使用同一根 `go.mod`。入口层只处理表现与进程交互，核心用例不依赖 CLI 框架或 Wails；Wails 当前尚未确定采用。

## 配置与凭据

- 非敏感配置使用 TOML。
- Windows 应用数据根目录为 `%LOCALAPPDATA%\JumpAccess`。
- macOS 应用数据根目录为 `~/Library/Application Support/JumpAccess`。
- Token 使用 Windows Credential Manager 或 macOS Keychain，不能写入 TOML、测试 fixture、日志或命令输出。macOS Keychain 的正式后端使用 CGO 直接链接系统 Security framework；关闭 CGO 的 macOS 交叉构建只用于编译检查，不具备凭据读写能力。
- Profile 范围内保存 Alias。修改配置时应支持用户直接批量编辑，并提供打开配置文件的快捷命令。
- 读取配置与构造外部客户端应显式发生在应用启动流程中，避免包初始化因缺少本机配置而失败。

配置文件名为 `config.toml`。当前 schema 版本为 `1`；Profile 保存在 `[profiles.<name>]`，Alias 保存在 `[profiles.<name>.aliases.<alias>]`。默认每 30 秒检查一次 Token，并在过期前 1 分钟刷新。长连接只启动独立的刷新监督器；刷新失败会报告告警，但不拥有也不取消活动 SSH Session。已知主机文件布局将在 SSH 实现后补充。

## CLI 与进程 I/O

- 普通交互命令可以使用 stdout/stderr 与用户沟通。
- `jumpctl proxy` 的 stdout 专用于 SSH 协议数据；日志、诊断和可操作错误只写 stderr。
- Proxy 模式不得启动浏览器或请求交互选择。认证或目标解析失败时返回明确的非零退出码。
- CLI 文档和代码使用通用 `ProxyCommand` 术语，不增加 Tabby 专用标志、配置字段或包。
- 错误信息和日志不得包含 Token、密码、Cookie、私钥或完整敏感响应。

## 测试约定

生产行为采用 RED–GREEN–REFACTOR：先添加能够说明行为的失败测试，确认失败原因正确，再实现最小改动并重构。

默认测试不需要真实 JumpServer 账号，优先覆盖：

- TOML、Profile 和 Alias 解析。
- OAuth PKCE、loopback callback、Token 过期、轮换和并发刷新。
- 使用本地 HTTP server 模拟 JumpServer API。
- 使用本地 SSH client/server 验证直接模式和 ProxyCommand 的协议边界。
- stdout、stderr、退出码以及敏感信息脱敏。
- Token 刷新失败不会关闭活动 SSH Session。

真实账号只用于开发者本机手工 smoke test，用来确认浏览器登录、MFA、真实 API、Connection Token 和 SSH 完整链路。账号、密码和 Token 不通过对话传递，不进入自动 CI；程序需要登录时，由开发者本人在系统浏览器中完成。

## 跨平台约定

- 将平台凭据存储和路径解析隔离在平台适配层，并为可离线验证的部分提供接口或替身。
- 不安装或依赖 Windows Service。
- 共享核心不能假定 Windows 路径语义；平台路径由对应适配实现计算。
- 形成真实构建入口后，至少验证 Windows 与 macOS 目标构建；具体架构和发布矩阵随发布流程确定。

## 当前验证入口

```powershell
go test ./...
go vet ./...
go build -trimpath ./cmd/jumpctl
```

每次代码修改结束前，运行与本次修改最相关的测试；在交付完整阶段前运行全量测试、静态检查和目标平台构建检查。只有真实执行成功的命令才能写成当前可用入口。
