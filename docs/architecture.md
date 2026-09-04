# 架构说明

## 当前状态

JumpAccess 已建立单一 Go module、`cmd/jumpctl` CLI 入口和 `cmd/jumpaccess` Wails 桌面入口。跨平台应用目录、严格 TOML 配置、OAuth Token 生命周期、JumpServer 连接准备协议、直接 SSH 客户端和通用 ProxyCommand SSH server façade 已由 CLI 使用；GUI 已接通类型化桌面应用 API、手工 OAuth 回调、进程生命周期内自动续期、分页资源与 Alias 管理、统一主题、可持久化 Tab 工作区、多 xterm SSH 会话和 Wails 事件桥接。Windows 使用前端自绘标题栏与窗口按钮，macOS 使用隐藏内嵌标题栏并保留原生 traffic lights。真实 JumpServer 与 macOS 原生环境仍需 smoke test。

## 系统范围与整体架构

JumpAccess 计划以单个 Go module `github.com/cmstar/jumpaccess` 承载共享能力，并允许多个可执行入口复用这些能力：

- `jumpctl` 既可以作为独立 SSH 客户端运行，也可以作为通用 SSH `ProxyCommand` 被外部客户端调用。
- `jumpaccess` 是基于 Wails 2 的桌面入口，负责认证、资源管理和直接 SSH，不提供 ProxyCommand 或其他代理能力。
- OAuth、配置、JumpServer API、目标解析、Token 管理和 SSH 能力应位于可复用核心中，不能依赖具体 CLI 或未来 GUI 的表现层。

本项目不安装 Windows Service，也不依赖 JumpServer 桌面 Client 才能完成认证或连接。JumpServer Client `v4.1.6` 仅作为首个协议与行为分析基线。

## 技术栈与技术选择

- 核心实现采用 Go，并保持单一 module。
- 桌面入口采用 Wails 2.14，前端采用 React、TypeScript 和 Vite；Wails 仅位于表现层，不进入共享核心。
- Wails 的 `bindings` build tag 入口只提供可反射的桌面适配器与默认窗口参数，不装配运行时服务或读取用户数据；真实桌面入口才解析应用目录并校验配置。
- OAuth Token 使用应用根目录内受严格权限保护的 Profile 独立文件；Windows Credential Manager 与 macOS Keychain 只承载 ProxyCommand host key。
- SSH、OAuth 和 JumpServer API 的具体 Go 依赖将在实现和测试时选定，本文不提前指定。

## 目标系统组成与模块职责

以下表格同时记录已建立的边界和后续目标；未实现项会明确标注：

| 逻辑部分 | 目标职责 |
| --- | --- |
| 入口适配器 | `cmd/jumpctl` 和 `internal/cli` 已建立；负责参数与进程 I/O，不承载 JumpServer 协议细节 |
| 应用层 | `internal/application/settings` 承载 Profile、Organization 与 Alias 修改，`internal/application/auth` 承载登录状态与 Token 生命周期，`internal/application/resources` 和 `internal/application/connect` 分别负责资源查询与连接编排 |
| 桌面应用层 | `internal/application/desktop` 把启动状态、Profile 认证摘要、Organization、分页 Asset、Account、Alias、快速搜索、GUI 偏好、Tab 工作区和许可证整理为 Wails 可绑定的类型化 API，并协调异步主机密钥确认；本地 Alias 搜索结果与远端 Asset 按 ID 去重 |
| 依赖装配 | `internal/bootstrap` 统一构造 CLI 与 GUI 共用的配置、HTTP、Token、认证、资源和连接服务；终端 I/O 与 ProxyCommand 仍由 CLI 适配层负责 |
| OAuth | `internal/oauth` 已实现 Discovery、Authorization Code + PKCE、严格 state 校验、浏览器启动、`jms://auth/callback` 手工回调、Token 获取、刷新与撤销；GUI 通过内存中的登录尝试完成“打开浏览器—粘贴回调—交换 Token”，发布版私有协议注册与进程间回调转交尚未实现 |
| 配置 | `internal/config` 已读取、严格校验并原子保存 TOML，管理 Profile、Alias 和非敏感行为配置 |
| GUI 偏好 | `internal/guiconfig` 独立读取和原子保存 `gui.toml`，按应用外观、终端和 Tab 分组承载主题、终端配色 ID、字体、右键与多行粘贴警告等交互、窗口状态和 Tab 顺序/活动项。内置 `terminal-schemes.json` 同时供 Go 校验与前端渲染读取；设置 UI 将终端样式与终端行为拆为同级面板，但持久化仍共用 `[terminal]`。SSH Tab 只保存重连所需描述符，不保存终端输出、live session ID 或秘密；该文件不进入 CLI 配置 schema |
| 系统字体 | `internal/systemfont` 隔离 Windows GDI 与 macOS CoreText 字体枚举，向桌面表现层提供已安装等宽字体族；不支持的平台返回空候选并由前端回退到通用 `monospace` 与手工输入 |
| 凭据存储 | `internal/credential` 已实现跨平台私有文件后端，并保留 Windows Credential Manager 与 macOS Keychain 作为 ProxyCommand host key 存储 |
| JumpServer 集成 | `internal/jumpserver` 已实现 Organization、Asset、Account、Connection Token 和 `jms://` client-url 协议；`internal/application/connect` 负责目标唯一性与连接准备 |
| SSH | `internal/sshclient` 提供 CLI 与 GUI 共用的可注入数据流会话；`internal/application/sshsession` 管理多个 GUI 会话、输入、窗口变化、取消、状态与批量输出；`internal/sshproxy` 将本地 SSH server session 映射到上游 SSH client channel；`internal/sshhostkey` 维护两层主机信任 |
| 桌面前端 | `cmd/jumpaccess/frontend` 使用 React 和 xterm.js 表现浏览器式 Tab 栏、Profile、Organization、分页 Asset、行内 Alias、GUI 偏好及多会话终端；纯 reducer 管理单例页和可重复 SSH Tab，生产环境只通过 Wails 绑定访问应用服务，Vite 开发服务器使用独立的内存预览适配器 |

## 关键数据流

### 浏览器登录

1. 用户执行独立的认证命令。
2. 程序生成 PKCE 和防伪状态，启动系统浏览器。
3. JumpServer 完成授权后生成 `jms://auth/callback`。当前开发版由用户把该链接或包含它的确认页 URL 粘贴回终端；发布版计划由已注册的私有协议处理程序自动接收。
4. 程序严格校验回调目标和 `state`，使用原登录进程持有的 PKCE verifier 交换 Token，并把敏感 Token 原子写入该 Profile 的私有凭据文件。
5. Profile、Alias 等非敏感信息继续保存在 TOML 配置中。

当前实现依据 `v4.1.6` 使用 OAuth Discovery、`write read` scopes、S256 PKCE 和服务器已登记的 `jms://auth/callback`。真实环境已确认服务器拒绝未登记的 `http://127.0.0.1:14876/auth/callback`，并能为 `jms://auth/callback` 生成外部跳转确认页；MFA、Token 交换与完整登录仍需继续验证。

当前开发版默认采用手工回调，`jumpctl auth login --manual` 和 GUI 的回调粘贴框使用同一套严格校验。GUI 只在进程内、限时保存 state 和 PKCE verifier，成功、取消或超时后清除，不写入配置或磁盘。正式发布后，默认模式计划注册 `jms` 私有协议：操作系统启动 callback 子进程，子进程通过受限于当前用户的本地 IPC 把原始 URL 交给等待中的登录进程，再由后者完成 state/PKCE 校验和换 Token。不安装 Windows Service，也不把 PKCE verifier 持久化。手工模式作为长期能力永久保留，支持官方客户端仍占用 `jms` 协议、设备策略禁止协议注册或用户主动不注册的环境。

同一操作系统用户下不能按 URL 路径把 `jms` scheme 同时路由给两个程序。发布版注册协议时必须检测现有处理程序、明确告知冲突且不得静默覆盖；选择手工模式时不修改协议注册。

### 连接准备与 SSH 会话

1. 应用根据当前 Profile、Organization、Asset、Account 和 Alias 解析唯一目标。
2. 在创建新连接前检查 Access Token；临近过期时使用 Refresh Token 刷新。多个 CLI 进程通过 Profile 级文件锁避免并发轮换 Refresh Token。
3. 应用通过 JumpServer API 获取创建 SSH 会话所需的短期连接信息。
4. SSH 会话建立后，其生命周期与 OAuth Access Token 解耦。后续 Token 刷新或刷新失败不得主动中断已有会话。

直接模式在 CLI 终端支持 Account 选择；GUI 从资产连接时若存在多个 Account 会先要求明确选择，从 Alias 连接时使用其绑定 Account，未绑定时同样要求选择。GUI 允许多个 SSH Tab 并行存在，通过 Wails 事件批量传递终端输出，并在桌面程序退出时关闭全部活动会话。活动 GUI Session 使用需要应答的 SSH keepalive global request 测量 JumpAccess 到 JumpServer SSH 网关的往返延迟，立即探测一次并每 3 秒更新；延迟通过独立 Wails 事件传递，不写入终端数据或持久化工作区，也不表示网关到最终 Asset 的链路耗时。远端断开或连接失败只会清理 live session，不会移除 Tab；终端追加 `Connection closed.` 与 `Press Enter to reconnect ...`，仅无修饰键 Enter 触发重连。两种直接模式在首次遇到未知 gateway 主机密钥时都显示 SHA-256 指纹并要求明确确认；GUI 的确认请求与具体会话 context 绑定，取消会话会解除等待。信任记录写入应用根目录下的 `known_hosts`；已知主机密钥变化始终失败。GUI 的 OAuth 刷新监督器使用独立 context，随桌面进程启动和停止，每轮重新读取配置与凭据并检查所有保存了 Refresh Token 的 Profile；它只为后续 API 请求维护 Token，不拥有 SSH client/session。

### 通用 ProxyCommand

兼容客户端通过 stdin/stdout 启动 `jumpctl proxy`。该模式的目标契约是：

- stdout 仅承载 SSH 协议数据；诊断信息只写入 stderr。
- 未登录、Refresh Token 失效、目标歧义等错误以明确诊断和非零退出码返回。
- Proxy 模式不触发需要人工操作的浏览器登录；用户应先通过独立认证命令登录。
- 功能和文档不与 Tabby 或其他单一客户端耦合。

当前实现不是普通 TCP 转发，而是先完成全部非交互 preflight，再在 stdin/stdout 上启动本地 SSH server façade，同时用 JumpServer 动态连接凭据建立已校验主机密钥的上游 SSH client。它转发 session channel 的 env、PTY、shell/exec、窗口变化、信号、stdout、extended stderr 和退出状态；拒绝端口转发、agent、X11、SFTP/subsystem 及未知请求。

ProxyCommand 有两层独立主机信任：

1. 外部 SSH 客户端验证 JumpAccess façade 的稳定 Ed25519 host key；私钥保存在操作系统安全凭据存储。
2. JumpAccess 使用应用根目录的 `known_hosts` 验证上游 JumpServer gateway。Proxy 模式不接受未知密钥，用户需先通过直接 SSH 审阅指纹。

## 数据与平台边界

每个用户使用单一应用根目录：

- Windows：`%LOCALAPPDATA%\JumpAccess`
- macOS：`~/Library/Application Support/JumpAccess`

共享的 Profile、Organization、Alias 和连接行为保存在根目录的 `config.toml`；应用外观、终端显示与交互、Tab 行为、窗口状态和 Tab 工作区单独保存在 `gui.toml`，CLI 不读取后者。窗口状态使用显示器标识、显示器工作区内的相对坐标和普通窗口尺寸保存；启动时先在隐藏状态下解析目标显示器、约束到当前可见工作区，再显示或最大化窗口。原显示器不存在时回退到主显示器居中，分辨率或工作区变化时收回越界部分。窗口最大化退出时保存最大化标记并保留最近的普通窗口边界；普通状态退出时更新显示器、坐标和大小；由自绘按钮最小化前记录普通边界，恢复获得焦点时校正到原显示器。工作区在 Tab 增删、切换或排序后串行保存；重启时恢复顺序与活动项，SSH Tab 一律以断连状态恢复且不自动连接。配置写入使用同一应用目录中的跨进程锁串行化 read-modify-write，避免并发修改相互覆盖。`known_hosts` 也位于该根目录下。OAuth Access Token 与 Refresh Token 位于 `credentials` 子目录，每个 Profile 对应一个 JSON 文件；文件名使用 `oauth/` 加精确 Profile 名的 SHA-256 摘要，因此不受文件系统非法字符、保留名或路径长度影响，也不会因字符替换发生碰撞。Profile 本身不按文件名规则规范化。

`credentials` 是敏感数据边界。Windows 为目录和文件设置不继承的受保护 DACL，只允许当前用户与 `SYSTEM`；macOS 要求目录归当前用户所有且权限为 `0700`，文件权限为 `0600`。读取时拒绝重解析点或符号链接、错误所有者和过宽权限；更新时在同目录创建私有临时文件、刷盘并原子替换。

OAuth Token 不读取或写入原生凭据存储。ProxyCommand façade 的稳定 Ed25519 host key 单独保存在原生凭据存储中，与 OAuth Token 文件分离。

## 外部集成

- JumpServer：提供 OAuth、资源查询和连接准备接口；首个参考基线为 Client `v4.1.6` 对应行为。
- 系统浏览器：承载用户授权和可能存在的 MFA 流程。
- 文件系统与平台 ACL：保存并保护 OAuth Token。
- Windows Credential Manager / macOS Keychain：仅保存 ProxyCommand host key。
- 支持 SSH `ProxyCommand` 的客户端：通过标准输入输出调用通用代理模式。

## 需要实现验证的边界

以下事项在编码阶段通过上游实现分析、模拟测试或本机 smoke test 验证，不应提前描述为已实现：

- OAuth MFA、Token 交换、私有协议注册/IPC 和 Refresh Token 轮换的真实服务器兼容性。
- Organization、Asset、Account、Connection Token 和连接 URL 的真实服务器兼容性与版本差异。
- ProxyCommand 与真实终端客户端的兼容性，以及窗口变化、信号和退出状态的真实环境表现。
- Windows 与 macOS 凭据文件权限及构建产物的实际行为。
