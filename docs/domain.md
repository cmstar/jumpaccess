# 业务说明

## 业务场景

JumpAccess 面向需要通过 JumpServer 访问远程 Asset 的用户。用户先独立完成 OAuth 登录，之后可以使用桌面 GUI 或 `jumpctl` 建立直接 SSH 会话，也可以让支持 SSH `ProxyCommand` 的客户端调用 `jumpctl` 作为连接通道。GUI 不提供代理能力。

JumpAccess 负责认证、资源发现、目标解析和连接编排；JumpServer 仍然是用户授权、Organization、Asset、Account 及会话访问策略的权威来源。

## 核心对象

| 对象 | 英文/代码名称 | 定义与关系 |
| --- | --- | --- |
| 连接配置 | Profile | 一个 JumpServer 站点及其本地连接上下文。Organization、Alias 和 Token 状态均在 Profile 范围内解释 |
| 组织 | Organization | JumpServer 中用于隔离资源和权限的组织上下文；资源查询和连接准备可能依赖当前 Organization |
| 资产 | Asset | 由 JumpServer 管理、可建立远程会话的目标资源 |
| 账号 | Account | 在某个 Asset 上用于建立会话的远程账号；同一 Asset 可能存在多个可用 Account |
| 别名 | Alias | 用户在某个 Profile 内为远程目标设置的稳定、本地可编辑名称；至少定位 Asset，也可绑定必要的 Organization 或 Account 以消除非交互连接歧义 |
| 访问令牌 | Access Token | 调用 JumpServer API 的短期 OAuth 凭据 |
| 刷新令牌 | Refresh Token | Access Token 即将过期或已经失效时，用于申请新 Token 的长期 OAuth 凭据 |
| 连接凭据 | Connection Token | JumpServer 为建立具体远程会话提供的短期连接信息；它与 OAuth Token 的用途不同 |
| SSH 会话 | SSH Session | 已经建立的数据通道。建立后不应依赖 OAuth Token 持续有效，也不因后台刷新失败而被主动关闭 |

Alias 固定归属于 Profile，必须定位一个 Asset，Account 可为空。Organization 不是独立归属：GUI 创建 Alias 时根据当前 Profile 的 Organization 查询并验证 Asset，再把该 Organization 与 Asset ID 一并保存；用户不能为同一 Alias 另选一个无关 Organization。绑定 Account 时只能从该 Asset 当前获准的 Account 中选择，清空 Account 表示连接时再询问。

## 关键流程

### 登录与续期

1. 用户选择 Profile 并执行认证命令。
2. JumpAccess 打开浏览器，完成 OAuth Authorization Code + PKCE 授权。
3. Token 写入当前用户私有的 Profile 独立凭据文件，TOML 中不保存秘密。
4. 程序启动或发起 API 请求时，根据 Token 有效期决定是否刷新。
5. 长时间运行时可以定期检查并刷新 Token，但刷新结果不得改变现有 SSH Session 的连接状态。

Refresh Token 已失效时，需要用户重新执行交互登录。Proxy 模式本身不打开浏览器。

当前 GUI 登录会打开系统浏览器，并要求用户把 JumpServer 确认页中的 `jms://` 链接或完整确认页 URL 粘贴回应用。登录尝试的 state 与 PKCE verifier 只保存在当前 GUI 进程内；私有协议注册与跨进程回调尚未实现。

### 目标解析

1. 根据显式参数或当前设置确定 Profile 和 Organization。
2. 优先在该 Profile 范围内解析 Alias，否则查询 JumpServer Asset。
3. 确定 Asset 和 Account。
4. 直接 SSH 模式可以在终端中对多个 Account 进行交互选择。
5. Proxy 模式必须非交互地得到唯一结果；Alias 未绑定 Account 或参数不能消除歧义时直接报错，不进行猜测。

### 建立连接

- 直接模式：`jumpctl` 承担 SSH 客户端角色，连接成功后向用户提供交互终端。
- Proxy 模式：`jumpctl` 作为通用 SSH `ProxyCommand` 进程，为外部兼容客户端传输 SSH 协议数据。

两种模式共享认证、资源查询、Alias 解析和 JumpServer 连接准备逻辑，但具有不同的交互约束。

## 业务规则

- Alias 位于 TOML 配置中，允许用户批量直接编辑；项目需要提供打开配置文件的快捷命令。
- GUI 的资产搜索同时匹配远端 Asset 与本地 Alias；合并结果按 Asset ID 去重。远端 Asset API 使用 offset/limit 分页，GUI 保留对应分页语义。
- “All organizations” 是聚合上下文：选择它时显示各具体 Organization 中与当前资产匹配的 Alias；在该聚合上下文创建的 Alias 切换到具体 Organization 后，只要对应 Asset 可见，也继续显示。
- GUI 在资产行内纵向展示该 Asset 的全部 Alias；只有完全没有 Alias 时才显示创建入口。资产数和当前 Organization 的 Alias 总数显示在对应表头，Alias 总数不受当前分页影响。
- GUI 从 Asset 发起连接且存在多个 Account 时必须让用户选择；从 Alias 发起连接时优先使用已绑定 Account，未绑定时同样询问，不能隐式挑选第一个 Account。
- Profile 名是用户可见的精确标识，不为适配文件系统进行替换或规范化；拒绝空名称、首尾空白、控制字符以及 `.`、`..`，其余名称通过稳定摘要映射到凭据文件。
- GUI 删除 Profile 会一并清除其 Server URL、Organization、全部 Alias 和本地 OAuth 凭据，并断开该 Profile 的活动 SSH Session；不会删除 JumpServer 上的 Asset 或 Account。删除当前 Profile 后按名称选择下一个 Profile，没有剩余项时回到未配置状态。
- Token、密码、Cookie 和私钥不得进入 TOML、日志或普通命令输出。
- Access Token 刷新只服务于后续 API 请求和新连接，不得主动终止已经建立的 SSH Session。
- Proxy 模式保持非交互：不打开浏览器、不在 stdout 输出提示、不在目标歧义时要求用户选择。
- Proxy 模式的失败原因写入 stderr 并返回非零退出码，使调用方能够区分未登录、认证过期、目标歧义和连接失败。
- 产品能力以通用 SSH `ProxyCommand` 契约定义；Tabby 只能作为可选配置示例，不能成为业务模型或实现依赖。

## 术语表

- **JumpAccess**：项目和产品名称。
- **`jumpctl`**：首个 CLI 可执行文件名称。
- **JumpServer Client `v4.1.6`**：首个接口和行为参考基线，不是运行时依赖。
- **直接模式**：`jumpctl` 自己承担 SSH 客户端角色。
- **Proxy 模式 / ProxyCommand 模式**：由外部 SSH 客户端启动 `jumpctl`，通过标准输入输出使用其中继能力。它不是“Tabby 模式”。
- **Profile**：本地 JumpServer 连接上下文；不要与 JumpServer Organization 混用。
- **Token**：泛称时可能包括 OAuth Token 与 Connection Token；实现和文档应尽量使用精确名称。
