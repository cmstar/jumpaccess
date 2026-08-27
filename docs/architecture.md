# 架构说明

## 当前状态

JumpAccess 尚处于工程初始化阶段。本文记录已经确认的目标架构和边界，不表示各模块已经实现；目录、依赖和协议细节应在编码与验证过程中据实更新。

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

以下是逻辑职责，不代表当前已经存在同名包：

| 逻辑部分 | 目标职责 |
| --- | --- |
| 入口适配器 | 解析 CLI 或未来 GUI 输入，调用共享用例，不承载 JumpServer 协议细节 |
| 应用层 | 编排登录、刷新、资源查询、目标解析和连接流程 |
| OAuth | Authorization Code + PKCE、浏览器启动、loopback callback、Token 获取与刷新 |
| 配置 | 读取和校验 TOML，管理 Profile、Alias 和非敏感行为配置 |
| 凭据存储 | Windows Credential Manager 与 macOS Keychain 的平台适配 |
| JumpServer 集成 | 调用 Organization、Asset、Account 和连接凭据相关接口 |
| SSH | 建立并维持直接 SSH 会话，以及实现通用 `ProxyCommand` 协议中继 |

## 关键数据流

### 浏览器登录

1. 用户执行独立的认证命令。
2. 程序生成 PKCE 和防伪状态，启动系统浏览器。
3. JumpServer 完成授权后回调本机 loopback listener。
4. 程序交换 Token，并把敏感 Token 写入操作系统安全凭据存储。
5. Profile、Alias 等非敏感信息继续保存在 TOML 配置中。

浏览器登录的具体端点、Scope、回调细节和错误契约仍需结合 `v4.1.6` 行为与真实环境验证后固化。

### 连接准备与 SSH 会话

1. 应用根据当前 Profile、Organization、Asset、Account 和 Alias 解析唯一目标。
2. 在创建新连接前检查 Access Token；临近过期时使用 Refresh Token 刷新。
3. 应用通过 JumpServer API 获取创建 SSH 会话所需的短期连接信息。
4. SSH 会话建立后，其生命周期与 OAuth Access Token 解耦。后续 Token 刷新或刷新失败不得主动中断已有会话。

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

- OAuth Discovery、Scope、Redirect URI 和 Refresh Token 轮换的实际契约。
- Organization、Asset、Account、Connection Token 和连接 URL 的字段与版本差异。
- ProxyCommand 协议桥接、终端窗口变化、信号和退出状态传播。
- Windows 与 macOS 凭据存储及构建产物的实际行为。
