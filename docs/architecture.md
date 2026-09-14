# 架构说明

## 当前状态

JumpAccess 已建立单一 Go module、`cmd/jumpctl` CLI 入口和 `cmd/jumpaccess` Wails 桌面入口。跨平台应用目录、严格 TOML 配置、OAuth Token 生命周期、JumpServer 连接准备协议、直接 SSH 客户端和通用 ProxyCommand SSH server façade 已由 CLI 使用；GUI 已接通类型化桌面应用 API、手工 OAuth 回调、进程生命周期内自动续期、分页资源与 Alias 管理、统一主题、可持久化 Tab 工作区、多 xterm SSH 会话、独立 SFTP 文件页和 Wails 事件桥接。Windows 使用前端自绘标题栏与窗口按钮，macOS 使用隐藏内嵌标题栏并保留原生 traffic lights。真实 JumpServer 与 macOS 原生环境仍需 smoke test。

## 系统范围与整体架构

JumpAccess 计划以单个 Go module `github.com/cmstar/jumpaccess` 承载共享能力，并允许多个可执行入口复用这些能力：

- `jumpctl` 既可以作为独立 SSH 客户端运行，也可以作为通用 SSH `ProxyCommand` 被外部客户端调用。
- `jumpaccess` 是基于 Wails 2 的桌面入口，负责认证、资源管理、直接 SSH 和 SFTP 文件传输，不提供 ProxyCommand 或其他代理能力。
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
| GUI 偏好 | `internal/guiconfig` 独立读取和原子保存 `gui.toml`，按应用外观、终端和 Tab 分组承载主题、终端配色 ID、字体、字号、行高、光标样式与闪烁、右键、多行粘贴警告与回车复制选区等交互、窗口状态和 Tab 顺序/活动项。内置 `terminal-schemes.json` 同时供 Go 校验与前端渲染读取；预览与会话共用终端渲染参数。设置 UI 将终端样式、终端背景图与终端行为拆为同级面板；背景图保存在 `[terminal.background]`，其余共用 `[terminal]`。SSH/SFTP Tab 只保存重连所需描述符，不保存终端输出、目录、传输队列、live session ID 或秘密；该文件不进入 CLI 配置 schema |
| 系统字体 | `internal/systemfont` 隔离 Windows GDI 与 macOS CoreText 字体枚举，向桌面表现层提供已安装等宽字体族；不支持的平台返回空候选并由前端回退到通用 `monospace` 与手工输入 |
| 凭据存储 | `internal/credential` 已实现跨平台私有文件后端，并保留 Windows Credential Manager 与 macOS Keychain 作为 ProxyCommand host key 存储 |
| JumpServer 集成 | `internal/jumpserver` 已实现 Organization、Asset、Account、Connection Token 和 `jms://` client-url 协议；`internal/application/connect` 负责资产首个匹配解析、账号选择与连接准备 |
| SSH | `internal/sshclient` 提供 CLI 与 GUI 共用的可注入数据流会话；`internal/application/sshsession` 管理多个 GUI 会话、输入、窗口变化、取消、状态与批量输出；`internal/sshproxy` 将本地 SSH server session 映射到上游 SSH client channel；`internal/sshhostkey` 维护两层主机信任 |
| SFTP | `internal/sftpclient` 在独立 SSH transport 上打开 SFTP subsystem，复用 gateway 主机信任；`internal/application/sftpsession` 管理目录、文件操作和进程内传输队列，流式 I/O 留在 Go，前端只接收元数据与进度 |
| 桌面前端 | `cmd/jumpaccess/frontend` 使用 React 和 xterm.js 表现浏览器式 Tab 栏、Profile、Organization、分页 Asset、行内 Alias、GUI 偏好及多会话终端；纯 reducer 管理单例页和可重复 SSH/SFTP Tab，生产环境只通过 Wails 绑定访问应用服务，Vite 开发服务器使用独立的内存预览适配器 |

## 关键数据流

浏览器演示通过独立的 Vite `demo` 模式加载真实 React 界面与 `createPreviewBackend` 内存实例，提供已配置、首次使用、登录过期场景；刷新页面重置数据。模拟 SSH 仅解释预设命令，SFTP 仅操作内存目录。演示模块不进入正常生产前端构建，也不装配 Go 服务或访问正式配置和凭据。桌面程序尚无演示启动参数。文档截图由独立 Playwright 脚本在人工触发时生成，输出到 `docs/screenshots`，不属于构建、测试或发布流程。

资产页和设置页的组件生命周期跟随对应 Tab 是否存在；切换时通过 `hidden` 隐藏页面并保留 DOM、滚动位置及设置导航状态，关闭 Tab 才卸载。不活跃页面不占布局或参与键盘焦点导航，临时视图状态不写入 `gui.toml`。

桌面 Asset DTO 同时保留类型、类别的稳定标识与显示名称；`AssetIcon` 只根据这些元数据选择图标，连接入口仍由授权协议决定。资产列表与详情复用 `AssetAliasItem` 和既有 Alias 应用操作，详情不维护独立的别名副本。详情加载失败在相应资产内显示并允许重试，与无授权协议状态区分。

新 Tab 的插入位置由 Tab reducer 统一处理，桌面前端传入当前 GUI 偏好 `tabs.new_tab_position`：`end` 追加到末尾，`after_current` 插入当前 Tab 右侧。单例页和 SSH/SFTP 页共用该规则；已存在单例页的激活及工作区恢复不重排 Tab。

终端当前使用 xterm DOM 渲染器。产品光标值 `quarter_block` 在共用渲染参数中映射为原生 `underline`，预览和会话宿主通过限定 CSS 将其加粗为字符格高度的约 25%，沿用原生光标颜色与闪烁。此适配依赖 DOM 光标结构；将来切换 Canvas/WebGL 渲染器或升级 xterm 时，必须重新验证或补充对应实现，不能直接把产品自定义值传给 xterm。

终端背景由应用级 Provider 统一加载，预览和 SSH 会话共用独立的背景容器。容器以终端方案底色打底，图片层单独设置透明度；xterm 创建时启用透明背景支持，图片加载成功后仅把默认背景的 alpha 设为 0，保留 RGB、ANSI 色、光标和选区参数。背景范围包含终端内容及内边距，排除外层框体和工具栏，固定在可视区域并不随输出滚动；切换图片或开关只更新显示，不重建终端和 SSH Session。图片失败时恢复纯色，设置页显示原因。

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

1. 应用根据当前 Profile、Organization、Asset、Account 和 Alias 解析连接目标；资产搜索逐页查找首个完整匹配，找到即停止，账号保留唯一性与交互选择规则。
2. 在创建新连接前检查 Access Token；临近过期时使用 Refresh Token 刷新。多个 CLI 进程通过 Profile 级文件锁避免并发轮换 Refresh Token。
3. 应用通过 JumpServer API 获取创建 SSH 会话所需的短期连接信息。
4. SSH 会话建立后，其生命周期与 OAuth Access Token 解耦。后续 Token 刷新或刷新失败不得主动中断已有会话。

直接模式在 CLI 终端支持 Account 选择；GUI 从资产连接时若存在多个 Account 会先要求明确选择，从 Alias 连接时使用其绑定 Account，未绑定时同样要求选择。GUI 允许多个 SSH Tab 并行存在，通过 Wails 事件批量传递终端输出，并在桌面程序退出时关闭全部活动会话。活动 GUI Session 使用需要应答的 SSH keepalive global request 测量 JumpAccess 到 JumpServer SSH 网关的往返延迟，立即探测一次并每 3 秒更新；延迟通过独立 Wails 事件传递，不写入终端数据或持久化工作区，也不表示网关到最终 Asset 的链路耗时。远端断开或连接失败只会清理 live session，不会移除 Tab；终端追加 `Connection closed.` 与 `Press Enter to reconnect ...`，仅无修饰键 Enter 触发重连。两种直接模式在首次遇到未知 gateway 主机密钥时都显示 SHA-256 指纹并要求明确确认；GUI 的确认请求与具体会话 context 绑定，取消会话会解除等待。信任记录写入应用根目录下的 `known_hosts`；已知主机密钥变化始终失败。GUI 的 OAuth 刷新监督器使用独立 context，随桌面进程启动和停止，每轮重新读取配置与凭据并检查所有保存了 Refresh Token 的 Profile；它只为后续 API 请求维护 Token，不拥有 SSH client/session。

### 桌面 ZMODEM

SSH ZMODEM 协议由前端 `lib/zmodem.ts` 的会话级控制器和 `zmodem.js` 0.1.10 实现。控制器在 App 层按 live session ID 管理，切换 Tab、重建 xterm 或历史回放不会重启传输。先收齐起始帧，协议字节不写入终端历史；用户选择上传文件，或后端按下载偏好取得目录授权后才确认握手、发送协议应答。

`sshsession` 对桌面输出使用 Base64 和有序序号，每块最多 32 KiB。前端解析并将下载内容交给 Go 端缓冲或文件写入后确认该块，后端再继续输出，形成到 SSH channel 的反压。普通文本使用流式 UTF-8 解码。CLI 使用独立的直接终端适配器。

`internal/application/zmodemfiles` 持有原生对话框授权的文件句柄，以及原生对话框或用户保存的自动下载偏好产生的目录授权；上传最多每次读取 64 KiB，下载协议块交给 Go 端 `bufio.Writer`，JS 不缓存整文件。所有 live Session 共用 50 MiB 的下载缓冲额度，单文件预留容量为声明大小与剩余额度的较小值；额度耗尽时直接写文件。能容纳的文件收齐后保存，较大文件在缓冲满后分批写入。额度限制活动下载缓冲，不包含运行时、桥接临时对象和操作系统缓存。下载拒绝路径分隔符、控制字符、Windows 设备名和 ADS，使用排他创建避免覆盖；同名文件添加数字后缀。完成时核对大小、Flush 后 Sync，成功才报告完成；取消、错误、断连及退出丢弃剩余缓冲，清理未完成文件和授权并释放额度。ZMODEM 的受限二进制块跨 Wails 桥接；独立 SFTP 的文件流仍全部留在 Go。

桌面 SSH 下载目录策略位于 `cmd/jumpaccess/download_directory.go`，复用 `internal/downloads` 的系统目录解析。GUI 偏好使用 `[downloads]` 配置分组，界面将“下载保存位置”放在“终端行为”面板内；支持固定从系统下载目录询问、自动下载到系统目录、记住上次目录询问、自动下载到指定目录。目录历史仅由 Go 端在选择并授权成功后以锁保护的 read-modify-write 更新，不发送到前端。`SavePreferences` 仅更新模式与指定目录，保留目录历史，避免与并发的会话选择或工作区保存互相覆盖。
`lib/zmodemProgress.ts` 将字节计数格式化为终端中的本地进度行，约每 100 ms 使用 CR 和清行刷新，本机完整文件路径单独一行并过滤控制字符。上传显示 `Upload <path>`，下载显示 `Download to <path>`；Go 文件元数据返回绝对 `path`，下载采用排他创建后实际文件名，包含同名另存后缀。协议上传仍仅发送 `name`，本机路径不进入 SSH 输入或 ZMODEM 文件元数据。应用生成的 SSH 终端文案统一使用英文，文件名和服务器原始输出不翻译；可能为中文的后端错误详情留在状态栏，终端显示英文失败提示。进度只进入显示和历史，不进入 SSH 输入或协议解析。会话控制器在整批传输结束、有成功文件且后端清理完成后发出一次完成回调，App 按完成回调所属的 live session ID 查找 SSH Tab，复用 Tab 标题规则优先取别名、无别名时取资产名称，将其加在消息前（如 `alias1 下载完成：xxx.txt`），通过通用 `showInfo` 显示顶部中央提示，不依赖当前活动 Tab，也不通过匹配状态文案推断成功。完成消息附带本批次成功文件的文件名，多文件以顿号连接；只使用 Go 返回的 `name`，不包含本机路径，下载同名另存时显示实际文件名。文件名列表按批次重置，上传被跳过的文件不计入。取消、失败、断连、空批次和上传全部跳过不发完成回调。收齐后仍显示 99% 及 `Saving` 或 `Waiting for confirmation`，完成保存或远端确认后才显示 100% 和 `Complete`；零字节文件同样等待完成。下一下载文件等待上一文件保存结束。为防止本地保存期间到达的 Shell 提示符被进度覆盖，普通输出暂存最多 64 Ki 字符，进度结束换行后恢复；超限时停止该文件的终端进度重画并立即恢复输出，底部状态栏继续计数。

能力探测在同一 SSH transport 上请求独立 `exec` channel，分别检查 PATH 中的 `rz` 和 `sz`，超时为 5 秒。响应不匹配或网关拒绝时为未知，不能当作未安装，也不向交互 PTY 自动写探测命令。手工命令握手能证实对应方向的能力。工具栏向当前 PTY 写入 `rz` 或引用后的单文件 `sz -- <path>`，用户需要处于 Shell 提示符。普通终端输入在传输期间由后端暂停，二进制协议输入单独放行。

文件选择最多等待 5 分钟，传输无数据活动最多等待 60 秒，可手工取消。每个 Session 同时传输一个批次，不持久化能力、进度或授权。真实 JumpServer 策略下的兼容性仍需本机 smoke test。

Windows 在 ZMODEM 原生文件选择器打开前和返回后，检查当前前台窗口是否属于本进程。指针隐藏且没有按住鼠标按钮时，向其 WebView 子窗口发送原生鼠标移动通知，由 Chromium 恢复指针可见性，规避 WebView2 152 的回归（[上游问题 #5687](https://github.com/MicrosoftEdge/WebView2Feedback/issues/5687)）。不移动实际鼠标、不修改系统设置或 `ShowCursor` 计数；通知超时有上限。非 Windows 不应用此兼容项。Wails 的 WebView2 loader 会清空额外浏览器环境参数，因此不能仅通过设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 关闭该功能。

### 独立 SFTP 会话

SFTP 连接准备使用 `protocol=sftp`、`connect_method=sftp_client`，并校验资产授权协议。连接到 client-url 返回的 gateway，使用短期 Connection Token 请求 `sftp` subsystem，不启动 Shell，也不复用 SSH Tab 的连接。两种连接共享 `known_hosts` 和主机密钥确认。

从 SSH 打开 SFTP 时，桌面适配器用原 Session 的 Profile、Organization、Asset ID 和 Account 固定连接身份。目录仅取打开时的 OSC 7 快照。KoKo 的 `/` 是平台配置的 SFTP 根目录；只有已知 `sftp_home` 的物理映射时，才把 SSH 目录换算为 SFTP 路径。无法映射或访问时静默尝试 home／起始目录，不假设 `/home/<username>`。文件浏览与终端此后独立。

上传和下载使用原生文件选择器及 Wails 文件拖入；Go 逐文件流式传输，冲突等待、取消和重试由会话队列负责。文件内容不跨 Wails 事件桥。传输临时文件在成功后发布为目标文件；递归操作跳过已识别的符号链接；针对 KoKo 的文件类型兼容行为，通过 READLINK 补充识别。关闭连接会取消其任务，切换 Tab 不影响任务；退出前有未完成任务时先请求前端确认。

文件类型检查兼容普通文件 READLINK 的两类返回：OpenSSH 将 `EINVAL` 编码为 `SSH_FX_BAD_MESSAGE`（`Bad message`），pkg/sftp 使用 `SSH_FX_FAILURE`；KoKo 可能再将上游 status 包装为一层 Failure。只有已知的非链接返回才继续 Lstat，权限拒绝、未实现和未知错误仍返回失败；READLINK 成功时保留符号链接类型，避免递归进入目标。此兼容仅用于 READLINK，不全局忽略下载或删除操作的 Bad message。

### CLI ZMODEM

`sshclient.Runner` 在 stdin/stdout 均为终端时，把输出交给 `internal/clitransfer`，默认识别 CRC 有效的起始帧。普通 SSH 字节不变；文件选择期间键盘仅进入本地路径提示，传输期间只处理本地 Ctrl+C，完成后恢复远端输入。用户选择期间若远端输出普通文本，则请求失效，不再向 Shell 发送协议应答。起始帧前缀等待上限为 1 秒，本地选择为 5 分钟，协议读写空闲为 60 秒；取消残留数据只在有限窗口内丢弃，并提示 Enter 刷新 Shell。写入串行且可取消，阻塞的远端写入通过关闭会话解除，输出解析提前退出时先关闭管道再等待 SSH 结束。

`internal/zmodem` 是独立 Go 协议层：接收 CRC16/CRC32，发送 CRC16，子包最多 8 KiB，发送确认窗口最多 1 MiB 并遵循远端缓冲声明；校验 CRC 和 32 位文件偏移，支持多文件下载、上传恢复偏移、有限重传及远端跳过。普通终端数据和远端协议在 Go 层分流，不运行 JavaScript，不依赖本机 `rz` / `sz`。下载复用 `zmodemfiles.Store` 的名称校验、排他创建、50 MiB 缓冲和清理规则；上传由本地明确选择的普通文件句柄提供可定位读取。文件小于 4 GiB，协议不提供目录传输。

`internal/downloads` 提供系统目录解析：Windows Known Folder 遵循用户重定向，macOS CGO 构建通过 Foundation 读取 Downloads；非原生构建回退到用户 `Downloads`，路径不可用时由 CLI 请求另选目录。`jumpctl ssh --download-dir` 只覆盖本次连接，不修改持久配置。非终端 I/O 和 `jumpctl proxy` 保持透传。

### 通用 ProxyCommand

兼容客户端通过 stdin/stdout 启动 `jumpctl proxy`。该模式的目标契约是：

- stdout 仅承载 SSH 协议数据；诊断信息只写入 stderr。
- 未登录、Refresh Token 失效、账号歧义等错误以明确诊断和非零退出码返回。
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

共享的 Profile、Organization、Alias 和连接行为保存在根目录的 `config.toml`；应用外观、终端显示与交互、Tab 行为、窗口状态和 Tab 工作区单独保存在 `gui.toml`，CLI 不读取后者。窗口状态使用显示器标识、显示器工作区内的相对坐标和普通窗口尺寸保存；启动时先在隐藏状态下解析目标显示器、约束到当前可见工作区，再显示或最大化窗口。原显示器不存在时回退到主显示器居中，分辨率或工作区变化时收回越界部分。窗口最大化退出时保存最大化标记并保留最近的普通窗口边界；普通状态退出时更新显示器、坐标和大小；由自绘按钮最小化前记录普通边界，恢复获得焦点时校正到原显示器。工作区在 Tab 增删、切换或排序后串行保存；重启时恢复顺序与活动项，SSH/SFTP Tab 一律以断连状态恢复且不自动连接。配置写入使用同一应用目录中的跨进程锁串行化 read-modify-write，避免并发修改相互覆盖。`known_hosts` 也位于该根目录下。OAuth Access Token 与 Refresh Token 位于 `credentials` 子目录，每个 Profile 对应一个 JSON 文件；文件名使用 `oauth/` 加精确 Profile 名的 SHA-256 摘要，因此不受文件系统非法字符、保留名或路径长度影响，也不会因字符替换发生碰撞。Profile 本身不按文件名规则规范化。

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
