# 架构说明

## 当前状态

JumpAccess 已建立单一 Go module、`cmd/jumpctl` 入口、跨平台应用目录、严格 TOML 配置、OAuth Token 生命周期、JumpServer 连接准备协议和直接 SSH 客户端。ProxyCommand 仍是目标设计，尚未实现。

## 系统范围与整体架构

JumpAccess 计划以单个 Go module `github.com/cmstar/jumpaccess` 承载共享能力，并允许多个可执行入口复用这些能力：

- `jumpctl` 是首个入口，既可以作为独立 SSH 客户端运行，也可以作为通用 SSH `ProxyCommand` 被外部客户端调用。
- 未来可以增加 GUI 入口。Wails 目前只是可能的实现方式，尚未被确定为依赖或交付范围。
- OAuth、配置、JumpServer API、目标解析、Token 管理和 SSH 能力应位于可复用核心中，不能依赖具体 CLI 或未来 GUI 的表现层。

本项目不安装 Windows Service，也不依赖 JumpServer 桌面 Client 才能完成认证或连接。JumpServer Client `v4.1.6` 仅作为首个协议与行为分析基线。

## 技术栈与技术选择

- 核心实现采用 Go，并保持单一 module。
- CLI 是首个确定的入口；Wails 只是未来 GUI 的候选方案，当前不作为依赖。
- Windows Credential Manager 与 macOS Keychain 是各自平台的敏感凭据存储边界。
- SSH、OAuth 和 JumpServer API 的具体 Go 依赖将在实现和测试时选定，本文不提前指定。

## 目标系统组成与模块职责

以下表格同时记录已建立的边界和后续目标；未实现项会明确标注：

| 逻辑部分 | 目标职责 |
| --- | --- |
| 入口适配器 | `cmd/jumpctl` 和 `internal/cli` 已建立；负责参数与进程 I/O，不承载 JumpServer 协议细节 |
| 应用层 | `internal/application/settings` 承载配置修改，`internal/application/auth` 承载登录状态与 Token 生命周期；查询和连接编排尚未实现 |
| OAuth | `internal/oauth` 已实现 Discovery、Authorization Code + PKCE、严格 state 校验、浏览器启动、loopback callback、Token 获取、刷新与撤销 |
| 配置 | `internal/config` 已读取、严格校验并原子保存 TOML，管理 Profile、Alias 和非敏感行为配置 |
| 凭据存储 | `internal/credential` 已适配 Windows Credential Manager；macOS CGO 构建直接调用 Keychain Security framework |
| JumpServer 集成 | `internal/jumpserver` 已实现 Organization、Asset、Account、Connection Token 和 `jms://` client-url 协议；`internal/application/connect` 负责目标唯一性与连接准备 |
| SSH | `internal/sshclient` 已建立直接 SSH 会话，`internal/sshhostkey` 维护严格的 gateway 主机信任；通用 `ProxyCommand` 协议中继尚未实现 |

## 关键数据流

### 浏览器登录

1. 用户执行独立的认证命令。
2. 程序生成 PKCE 和防伪状态，启动系统浏览器。
3. JumpServer 完成授权后回调本机 loopback listener。
4. 程序交换 Token，并把敏感 Token 写入操作系统安全凭据存储。
5. Profile、Alias 等非敏感信息继续保存在 TOML 配置中。

当前实现依据 `v4.1.6` 使用 OAuth Discovery、`write read` scopes、S256 PKCE 和固定 `http://127.0.0.1:14876/auth/callback`。真实环境仍需验证服务器登记的 Redirect URI、MFA 和端到端登录行为。

### 连接准备与 SSH 会话

1. 应用根据当前 Profile、Organization、Asset、Account 和 Alias 解析唯一目标。
2. 在创建新连接前检查 Access Token；临近过期时使用 Refresh Token 刷新。多个 CLI 进程通过 Profile 级文件锁避免并发轮换 Refresh Token。
3. 应用通过 JumpServer API 获取创建 SSH 会话所需的短期连接信息。
4. SSH 会话建立后，其生命周期与 OAuth Access Token 解耦。后续 Token 刷新或刷新失败不得主动中断已有会话。

直接模式在终端支持 Account 选择，并在首次遇到未知 gateway 主机密钥时显示 SHA-256 指纹要求确认。信任记录写入应用根目录下的 `known_hosts`；已知主机密钥变化始终失败。OAuth 刷新监督器使用独立 context，只为后续 API 请求维护 Token，不拥有 SSH client/session。

### 通用 ProxyCommand

兼容客户端通过 stdin/stdout 启动 `jumpctl proxy`。该模式的目标契约是：

- stdout 仅承载 SSH 协议数据；诊断信息只写入 stderr。
- 未登录、Refresh Token 失效、目标歧义等错误以明确诊断和非零退出码返回。
- Proxy 模式不触发需要人工操作的浏览器登录；用户应先通过独立认证命令登录。
- 功能和文档不与 Tabby 或其他单一客户端耦合。

JumpServer 动态连接凭据与下游 SSH 协议之间的桥接方式将在协议原型和集成测试中确认；不能退化为未经验证的普通 TCP 转发。

## 数据与平台边界

非敏感应用数据使用单一应用根目录：

- Windows：`%LOCALAPPDATA%\JumpAccess`
- macOS：`~/Library/Application Support/JumpAccess`

TOML 配置、已知主机等非敏感文件位于该根目录下。Access Token 和 Refresh Token 不写入这个目录，而是分别交给 Windows Credential Manager 或 macOS Keychain；这是操作系统安全存储边界，不是第二个应用数据目录。

## 外部集成

- JumpServer：提供 OAuth、资源查询和连接准备接口；首个参考基线为 Client `v4.1.6` 对应行为。
- 系统浏览器：承载用户授权和可能存在的 MFA 流程。
- Windows Credential Manager / macOS Keychain：保存敏感 Token。
- 支持 SSH `ProxyCommand` 的客户端：通过标准输入输出调用通用代理模式。

## 需要实现验证的边界

以下事项在编码阶段通过上游实现分析、模拟测试或本机 smoke test 验证，不应提前描述为已实现：

- OAuth Redirect URI、MFA 和 Refresh Token 轮换的真实服务器兼容性。
- Organization、Asset、Account、Connection Token 和连接 URL 的真实服务器兼容性与版本差异。
- ProxyCommand 协议桥接、终端窗口变化、信号和退出状态传播。
- Windows 与 macOS 凭据存储及构建产物的实际行为。
