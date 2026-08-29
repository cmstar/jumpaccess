# 开发说明

## 当前阶段

Go 工程、配置能力、OAuth Token 生命周期、JumpServer 连接准备协议、直接 SSH 和通用 ProxyCommand 已经建立。真实 JumpServer 和 macOS 原生环境仍待 smoke test；实际状态以测试和当前命令帮助为准。

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
internal/application/resources/# Organization、Asset 与 Account 查询
internal/cli/       # CLI 参数和输出适配
internal/config/    # TOML 模型、校验和存储
internal/credential/# 私有文件凭据与原生凭据兼容适配
internal/filelock/  # 多进程 Token 刷新锁
internal/jumpserver/# JumpServer REST 与 client-url 协议客户端
internal/oauth/     # OAuth Discovery、PKCE、callback 与 Token 协议
internal/sshclient/ # 直接 SSH 客户端会话
internal/sshhostkey/# SSH gateway 主机密钥信任
internal/sshproxy/  # 本地 SSH server 与上游 session 桥接
internal/sshupstream/ # 共享上游 SSH gateway 拨号
internal/stdioconn/ # ProxyCommand stdin/stdout 的 net.Conn 适配
internal/systemopen/# 打开配置文件的平台适配
internal/target/    # Profile、Alias 和远程目标解析
internal/terminalprompt/ # Account 与主机密钥的直接模式提示
docs/               # 长期项目知识
```

未来 GUI 可以增加独立入口，但项目继续使用同一根 `go.mod`。入口层只处理表现与进程交互，核心用例不依赖 CLI 框架或 Wails；Wails 当前尚未确定采用。

## 配置与凭据

- 非敏感配置使用 TOML。
- Windows 应用数据根目录为 `%LOCALAPPDATA%\JumpAccess`。
- macOS 应用数据根目录为 `~/Library/Application Support/JumpAccess`。
- OAuth Token 以每个 Profile 一个 JSON 文件保存在应用根目录的 `credentials` 子目录，不能写入 TOML、测试 fixture、日志或命令输出。Windows 使用受保护 DACL，macOS 使用 `0700` 目录和 `0600` 文件，并在读取时校验路径类型、所有者和权限。
- Profile 名不按文件名规则清洗或规范化；配置拒绝空名称、首尾空白、控制字符以及 `.`、`..`，凭据后端使用 `SHA-256("oauth/" + profile)` 生成固定长度文件名。这样允许 Unicode 和文件系统保留字符，并避免字符替换规则造成确定性碰撞。
- Windows Credential Manager 与 macOS Keychain 只用于 ProxyCommand host key 和旧 OAuth 凭据兼容读取。旧 Token 在下一次成功登录或刷新写入文件后删除；`auth logout` 同时删除文件与旧原生条目。macOS Keychain 后端使用 CGO 直接链接系统 Security framework；关闭 CGO 的 macOS 交叉构建仍可读写 OAuth 文件，但不能加载或创建 ProxyCommand façade host key。
- Profile 范围内保存 Alias。修改配置时应支持用户直接批量编辑，并提供打开配置文件的快捷命令。
- 读取配置与构造外部客户端应显式发生在应用启动流程中，避免包初始化因缺少本机配置而失败。

配置文件名为 `config.toml`。当前 schema 版本为 `1`；Profile 保存在 `[profiles.<name>]`，Alias 保存在 `[profiles.<name>.aliases.<alias>]`。默认每 30 秒检查一次 Token，并在过期前 1 分钟刷新；凭据更新使用同目录临时文件和原子替换。长连接只启动独立的刷新监督器；刷新失败会报告告警，但不拥有也不取消活动 SSH Session。SSH gateway 信任记录位于同一应用根目录的 `known_hosts`。

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
- OAuth PKCE、原生 `jms` callback、JumpServer 确认页 URL、Token 过期、轮换和并发刷新。
- 使用本地 HTTP server 模拟 JumpServer API。
- 使用本地 SSH client/server 验证直接模式和 ProxyCommand 的协议边界。
- stdout、stderr、退出码以及敏感信息脱敏。
- Token 刷新失败不会关闭活动 SSH Session。

真实账号只用于开发者本机手工 smoke test，用来确认浏览器登录、MFA、真实 API、Connection Token 和 SSH 完整链路。账号、密码和 Token 不通过对话传递，不进入自动 CI；程序需要登录时，由开发者本人在系统浏览器中完成。

当前开发版 OAuth smoke test 使用：

```powershell
& $Jump auth login --profile $Profile --manual
```

预期浏览器完成授权后进入外部跳转确认页。不要点击“确认”；复制页面内的 `jms://auth/callback?...` 链接或地址栏完整 URL，粘贴到终端的 `OAuth callback URL:` 提示后回车。成功时命令输出 `authenticated profile <name>`；随后 `auth status` 应显示已认证、Access Token 过期时间及 Refresh Token 可用性。测试记录不得保存回调 URL、Authorization Code 或 Token。

## 跨平台约定

- 将平台文件权限、原生凭据存储和路径解析隔离在平台适配层，并为可离线验证的部分提供接口或替身。
- 不安装或依赖 Windows Service。
- 共享核心不能假定 Windows 路径语义；平台路径由对应适配实现计算。
- 形成真实构建入口后，至少验证 Windows 与 macOS 目标构建；具体架构和发布矩阵随发布流程确定。

## 许可证与发布物

- JumpAccess 自身采用根目录 `LICENSE` 中的 MIT License；README 只使用链接到该文件的许可证徽章，不另设重复章节。
- 当前 Windows 和 macOS 生产依赖使用 MIT、Apache-2.0 或 BSD-3-Clause，没有 GPL、AGPL、LGPL 等 copyleft 依赖。依赖版本、版权声明和完整条款汇总在 `THIRD-PARTY-NOTICES.txt`。
- `LICENSE` 和 `THIRD-PARTY-NOTICES.txt` 通过 Go `embed` 编译进 `jumpctl`，`jumpctl licenses` 必须在单个可执行文件中保持可用；普通 `go build` 不需要复制额外文件即可保留可读声明。
- 正式 ZIP、tar 或安装包仍应把两份文本作为独立文件一并分发，方便不执行程序的接收者阅读。嵌入是单文件分发的保障，不替代正式归档中的显式材料。
- 新增或升级生产依赖时，必须重新检查目标平台的实际 package graph，更新第三方声明，并验证 `jumpctl licenses`。只用于测试、文档生成且不进入发布二进制的模块不需要混入发布声明。

## 当前验证入口

```powershell
go test ./...
go vet ./...
go build -trimpath ./cmd/jumpctl
```

每次代码修改结束前，运行与本次修改最相关的测试；在交付完整阶段前运行全量测试、静态检查和目标平台构建检查。只有真实执行成功的命令才能写成当前可用入口。
