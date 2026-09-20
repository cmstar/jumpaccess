# 开发说明

## 当前阶段

Go 工程、CLI 配置能力、OAuth Token 生命周期、JumpServer 连接准备协议、直接 SSH 和通用 ProxyCommand 已经建立；Wails GUI 已接通类型化资源 API、手工 OAuth 回调、进程生命周期内自动续期、Profile/Organization、分页资产与行内 Alias 管理、GUI 偏好、可持久化 Tab 工作区、无标准标题栏窗口、多 xterm SSH 会话和独立 SFTP 文件传输。真实 JumpServer 和 macOS 原生环境仍待 smoke test；实际状态以测试和当前命令帮助为准。

## 技术栈、版本与开发约束

- 主要实现语言：Go 1.25 或更高版本，以根目录 `go.mod` 为准。
- Module 路径：`github.com/cmstar/jumpaccess`。
- 工程采用单个 Go module，支持多个入口适配器共享核心逻辑。
- 可执行入口包括 `jumpctl` 与 `jumpaccess`；GUI 不得迫使 OAuth、配置、JumpServer API、目标解析或 SSH 逻辑复制一份。
- GUI 使用 Wails 2.14，前端使用 React、TypeScript 和 Vite。Wails 项目根位于 `cmd/jumpaccess`，与根 Go module 共用依赖。
- 桌面应用图标的唯一设计源为 `cmd/jumpaccess/build/appicon.svg`；`appicon.png` 是由该 SVG 渲染的 1024×1024 构建输入，Windows 多尺寸图标为 `cmd/jumpaccess/build/windows/icon.ico`。三者使用同一“终端 chevron 穿过跳板网关”标记，不以生成式栅格图作为发布源。
- Windows 和 macOS 都是目标平台。
- JumpServer Client `v4.1.6` 是首个协议行为参考，不应成为运行时依赖。

## 目标目录边界

当前目录边界：

```text
cmd/jumpctl/        # CLI 入口适配器
cmd/jumpaccess/     # Wails 桌面入口、前端与平台构建资源
internal/appdir/    # 单一应用数据根目录
internal/application/settings/ # Profile 与 Alias 修改用例
internal/bootstrap/ # CLI 与 GUI 共用的依赖装配
internal/application/auth/     # 登录状态、刷新与生命周期编排
internal/application/resources/# Organization、Asset 与 Account 查询
internal/application/desktop/  # Wails 使用的类型化桌面应用 API
internal/application/sshsession/ # GUI 多 SSH 会话与批量输出管理
internal/application/sftpsession/ # GUI SFTP 目录、文件操作与传输队列
internal/cli/       # CLI 参数和输出适配
internal/clitransfer/ # CLI ZMODEM 检测、本地输入切换和进度
internal/zmodem/    # Go ZMODEM 协议收发和 CRC 校验
internal/downloads/ # 系统下载目录查询
internal/config/    # TOML 模型、校验和存储
internal/guiconfig/ # GUI 独有偏好与 gui.toml 存储
internal/credential/# 私有文件凭据与原生凭据兼容适配
internal/filelock/  # 多进程配置、凭据生命周期和主机信任锁
internal/jumpserver/# JumpServer REST 与 client-url 协议客户端
internal/oauth/     # OAuth Discovery、PKCE、callback 与 Token 协议
internal/proxyconsole/ # Windows ProxyCommand 私有控制台脱离适配
internal/sshclient/ # 直接 SSH 客户端会话
internal/sftpclient/# 独立 SFTP subsystem、路径映射与文件流
internal/sshhostkey/# SSH gateway 主机密钥信任
internal/sshproxy/  # 本地 SSH server 与上游 session 桥接
internal/sshupstream/ # 共享上游 SSH gateway 拨号
internal/stdioconn/ # ProxyCommand stdin/stdout 的 net.Conn 适配
internal/systemfont/# Windows/macOS 已安装等宽字体枚举
internal/systemopen/# 打开配置文件的平台适配
internal/target/    # Profile、Alias 和远程目标解析
internal/terminalprompt/ # Account 与主机密钥的直接模式提示
docs/               # 长期项目知识
```

桌面入口继续使用根目录的同一个 `go.mod`。`cmd/jumpaccess/frontend` 保存 React 前端，`cmd/jumpaccess/build` 保存 Windows 与 macOS 构建资源；入口层只处理表现与进程交互，核心用例不依赖 CLI 框架或 Wails。

## 配置与凭据

- 非敏感配置使用 TOML。
- Windows 应用数据根目录为 `%LOCALAPPDATA%\JumpAccess`。
- macOS 应用数据根目录为 `~/Library/Application Support/JumpAccess`。
- OAuth Token 以每个 Profile 一个 JSON 文件保存在应用根目录的 `credentials` 子目录，不能写入 TOML、测试 fixture、日志或命令输出。Windows 使用受保护 DACL，macOS 使用 `0700` 目录和 `0600` 文件，并在读取时校验路径类型、所有者和权限。
- Profile 名不按文件名规则清洗或规范化；配置拒绝空名称、首尾空白、控制字符以及 `.`、`..`，凭据后端使用 `SHA-256("oauth/" + profile)` 生成固定长度文件名。这样允许 Unicode 和文件系统保留字符，并避免字符替换规则造成确定性碰撞。
- Windows Credential Manager 与 macOS Keychain 只用于 ProxyCommand host key，不参与 OAuth Token 读写。macOS Keychain 后端使用 CGO 直接链接系统 Security framework；关闭 CGO 的 macOS 交叉构建仍可读写 OAuth 文件，但不能加载或创建 ProxyCommand façade host key。
- macOS 使用 `SecItemCopyMatching`、`SecItemAdd`、`SecItemUpdate`、`SecItemDelete`，保持文件型 Keychain、`JumpAccess:` service 前缀和空 account，兼容旧版本条目，不切换到 Data Protection Keychain。原生回归在 macOS 上运行 `JUMPACCESS_TEST_KEYCHAIN=1 CGO_ENABLED=1 CGO_CFLAGS='-Werror=deprecated-declarations' go test ./internal/credential -run TestNativeKeychainLegacyCompatibility -count=1`；该测试会在默认 Keychain 创建并清理唯一命名的合成条目，可能触发系统授权提示，默认不运行。它覆盖传统条目读取、更新、二进制与空值、新增、删除和未找到语义。
- Profile 范围内保存 Alias。修改配置时应支持用户直接批量编辑，并提供打开配置文件的快捷命令。
- 读取配置与构造外部客户端应显式发生在应用启动流程中，避免包初始化因缺少本机配置而失败。
- Wails 生成 bindings 时会编译并执行带 `bindings` build tag 的临时程序；该模式只构造用于类型反射的桌面适配器，不得解析应用目录、读取用户配置或凭据、创建外部客户端。严格配置校验只属于真实应用启动流程，构建结果不能依赖构建机上的 JumpAccess 用户数据。
- 桌面程序在 Wails 窗口初始化前发生启动错误时，必须同时写入 `stderr` 并通过 Windows/macOS 原生错误对话框告知用户；bindings 临时程序不得显示该业务对话框。

共享配置文件名为 `config.toml`。当前 schema 版本为 `1`；Profile 保存在 `[profiles.<name>]`，Alias 保存在 `[profiles.<name>.aliases.<alias>]`。GUI 独有偏好保存在同目录的 `gui.toml`，当前 schema 版本为 `13`：`[appearance]` 只保存应用主题，`[terminal]` 保存终端配色 ID（`color_scheme`，默认 `nord`）、字体、字号、行高（`line_height`）、光标样式（`cursor_style`）、闪烁（`cursor_blink`）、滚动条显示方式（`scrollbar_visibility`，默认 `active`）、SSH 状态栏可见性（`show_status_bar`，默认 `true`）、右键行为、多行粘贴警告和回车复制选区（`copy_on_enter`，默认 `true`），`[downloads]` 保存桌面 SSH 下载行为（`mode`：`ask` / `automatic` / `remember` / `custom`，默认 `ask`）、指定目录（`directory`）及后端维护的上次选择目录（`last_directory`）；旧配置缺少该分组时采用默认值。`[tabs]` 保存新 Tab 打开位置（`new_tab_position`，仅接受 `end` / `after_current`，默认 `end`）、Tab 关闭按钮显示和关闭活动会话确认，此外还包含窗口最大化/普通边界、Tab 顺序、活动项和 SSH/SFTP 重连描述符；CLI 不读取该文件。读取 schema v1/v2 时保留原有主题、终端字体、字号、Tab 偏好、工作区和窗口位置，读取 schema v3–v12 时保留原有分组偏好，均在内存中补全为 v13；缺少配色时使用 Nord，缺少行高、光标样式和闪烁时分别使用 1.0、`block` 和 `true`。行高必须是 1.0–2.0 之间的有限数值，光标样式只接受 `block`、`bar`、`underline`、`quarter_block`；显式 `cursor_blink = false` 必须保留。v1–v3 缺少多行粘贴警告时默认开启，v4 及以后已有的关闭值必须保留。读取本身不重写文件；下次保存采用 v13。v1 在 Windows 保存的是虚拟桌面绝对坐标，在 macOS 保存的是当前显示器相对坐标，读取时仍保留原值，由窗口恢复逻辑按平台解释。终端输出、SFTP 当前目录、传输队列、live session ID 与运行状态不得写入 `gui.toml`。最大化或最小化退出时不得用临时窗口边界覆盖已保存的普通窗口边界。GUI 偏好和工作区写入也必须串行化 read-modify-write，防止并发保存互相覆盖。 “终端样式”中的“显示滚动条”下拉框依次提供“始终显示”（`always`）、“仅活跃时显示”（`active`，默认）和“隐藏”（`hidden`）。废弃未发布的布尔字段 `show_scrollbar`，不保留兼容解码；出现该旧字段时由本机手工替换，`true` 对应 `active`，`false` 对应 `hidden`。新字段只接受这三种值，选择即时作用于预览和已有 SSH 终端，隐藏自绘滚动条的完整槽位（遗留原生滚动条始终移除），保留历史缓冲区、滚轮操作与会话，不增加事件监听。`TerminalFitAddon` 按样式中的实际滚动条宽度计算列数，隐藏时回收全部预留宽度；模式变化会重新计算会话和两个设置预览的尺寸，不重建终端。 “显示 SSH 状态栏”开关位于“终端样式”中“显示滚动条”之后，关闭时立即隐藏所有 SSH Tab 的底部状态栏并回收 25px 空间，复用现有 ResizeObserver 调整终端网格；不重建终端、不影响顶部传输区域或 SFTP 状态栏。缺少该字段的旧配置默认开启，显式 false 必须在保存和重启后保留；保存失败沿用偏好队列恢复最近成功值。

终端滚动条统一使用 xterm 自绘控件，移除遗留原生滚动条的槽位和上下箭头。宽度由宿主的 `--terminal-scrollbar-width` 定义为 7px（原自绘控件宽度的一半），槽底使用当前方案的不透明背景色，滑块在普通、悬停和拖拽状态下均使用普通文字色与约 50% 透明度。背景图不改变槽底和滑块的配色；会话、终端样式预览和背景图预览共用这些规则，不增加事件监听或修改历史缓冲区。 两个设置预览保留最多 100 行历史，并重复三组配色示例形成可滚动范围；预览禁用输入且不抢焦点。`always` 模式固定显示滚动条，`active` 模式沿用 xterm 的交互显示、闲置隐藏行为，`hidden` 模式隐藏整个槽位；所有模式均可使用滚轮，不重建终端。

终端背景图保存在 `[terminal.background]`：`enabled` 默认 false，`file_path` 默认空，`transparency_percent` 为 0–95 的整数（默认 70），`fit_mode` 为 `cover` / `contain` / `stretch` / `tile`（默认 `cover`），`position_x_percent`、`position_y_percent` 为 0–100 的整数（默认 50），`tile_fit_long_edge`、`tile_only_whole_tiles` 默认 false。旧版本缺少该分组时补齐默认值；显式 0 和 false 必须保留。图片由 Wails 原生对话框选择，Go 仅读取普通图片文件并检查 20 MiB 上限和 PNG/JPEG/WEBP/GIF MIME；浏览器负责解码并拒绝超过 4000 万像素的图片。路径只保存在本机 GUI 偏好，图片不复制到应用目录、不写入 TOML、不发送到远端。前端应用级 Provider 共享当前图片，切换 Tab 或调整显示参数不重复读取；仅平铺模式增加容器 ResizeObserver。滑块拖动期间本地预览，释放指针、键盘操作结束或失焦后沿用已有串行保存队列落盘。

内置终端方案的唯一数据源为 `internal/guiconfig/terminal-schemes.json`，Go 通过 embed 校验合法 ID，前端静态导入后经 `model/terminalTheme.ts` 统一适配到 xterm。方案在构建时打包，运行时不联网下载。当前共 28 款，包括 Nord、Dracula、Catppuccin 四款、Tokyo Night 四款、所采用固定版本的全部 16 款 Windows Terminal 内置配色（Campbell、Campbell Powershell、CGA、Dark+、Dimidium、IBM 5153、Ottosson、Vintage、One Half 深浅两款、Solarized 深浅两款、Tango 深浅两款、VSCode Modern 深浅两款），以及 Ubuntu 22.04 深浅两款。Ubuntu 方案来自 `ubuntu/WSL` 官方发行配置片段，显示名称保留 `Ubuntu-22.04-ColorScheme` 和 `Ubuntu-22.04-Light-ColorScheme`；它们由发行版提供，不属于 Windows Terminal 默认配色文件。数据记录固定提交来源与许可证；Tokyo Night 使用 Apache-2.0，其余使用 MIT。Windows Terminal 字段已转为 xterm 字段，缺少选区颜色时派生半透明前景色，浅色方案补充反色选区前景以保持可辨识；新增方案的显式不透明选区使用对比清晰的文字颜色：默认使用终端底色，VSCode Modern 深浅两款均保留终端前景色，避免文字与选区底色过于接近。普通/明亮 ANSI 色值保留来源原值。新增方案时应同步检查授权、版权及变更说明，并更新 `THIRD-PARTY-NOTICES.txt`。

默认每 30 秒检查一次 Token，并在过期前 1 分钟刷新；凭据更新使用同目录临时文件和原子替换。`jumpctl ssh`、`jumpctl proxy` 和桌面 GUI 使用独立的刷新监督器；GUI 监督器覆盖所有保存了 Refresh Token 的 Profile，每轮重新读取配置和凭据以发现运行期间的变化，并在桌面程序退出时取消。刷新失败会报告告警，但监督器不拥有也不取消活动 SSH Session。SSH gateway 信任记录位于同一应用根目录的 `known_hosts`。

## CLI 与进程 I/O

- CLI 使用 Cobra 的唯一前缀匹配，进程级开关只初始化一次。`internal/cli/prefix.go` 为命令组统一校验未匹配的子命令，并校验 `help` 的目标路径；内置 `help`、`completion` 在构建命令树时初始化。新增命令需考虑同层级名称和别名的前缀冲突，测试完整名称优先、唯一缩写、歧义不执行及位置参数和选项值保持原样。
- 普通交互命令可以使用 stdout/stderr 与用户沟通。
- `jumpctl proxy` 的 stdout 专用于 SSH 协议数据；日志、诊断和可操作错误只写 stderr。
- Proxy 模式不得启动浏览器或请求交互选择。认证或目标解析失败时返回明确的非零退出码。
- Windows 的 `proxy` 在连接准备和本地 SSH façade 握手期间保留 stderr；握手成功后，只有 stdin、stdout 均不是 Console 句柄且当前控制台仅附着 `jumpctl` 一个进程，才以 best-effort 调用 `FreeConsole`。共享终端、交互句柄或任何检查失败时保持附着；非 Windows 平台不执行该优化。
- CLI 文档和代码使用通用 `ProxyCommand` 术语，不增加 Tabby 专用标志、配置字段或包。
- 错误信息和日志不得包含 Token、密码、Cookie、私钥或完整敏感响应。

## GUI 公共提示

开发调试分组位于 `cmd/jumpaccess/frontend/src/components/DeveloperSettings.tsx`，后续界面演示可在该组件内按功能添加 `settings-group`。`SettingsView` 仅在后端 Bootstrap 版本精确为 `dev` 时渲染该分组，并使用同一判断过滤导航和滚动定位列表。源码默认 `main.version = "dev"`，本地默认 `wails build` 保留此值；tag 发布工作流通过 `-ldflags "-X main.version=<version>"` 注入版本后隐藏入口，无需运行时访问 Git，也不根据 Vite 开发服务器状态判断。此开关只控制入口，不作为安全边界或编译期代码剔除机制。

全局提示由 `cmd/jumpaccess/frontend/src/components/Notifications.tsx` 的 `NotificationProvider` 在 `App` 外层挂载，各子组件通过 `useNotifications()` 的 `showInfo`、`showWarning`、`showError` 复用，无需逐层传递回调。浮层经 Portal 挂到 `document.body`，共用明暗主题，位于标题栏下方且高于 Modal；容器空白区域不拦截指针，长文本换行，过多提示可滚动。`useCopyText()` 统一普通文本复制及其结果反馈，不在提示中回显复制内容。表单和会话内的上下文错误继续由原组件管理，不重复弹出全局提示。测试使用模拟计时器验证自动关闭、暂停和卸载清理，使用可控 Promise 验证异步完成、失败及过期结果隔离。

## 测试约定

跨入口架构回归应从共享用例验证行为，并补对应 Adapter 的最小集成测试：账号歧义不得因 CLI/GUI 不同而变化；登录完成、退出和修改 Profile 必须遵守同一凭据锁；旧站点 Token 不得传入新站点客户端；主机密钥确认期间其他进程写入信任后必须重新校验。测试只用临时目录和合成凭据。

异步界面回归用可控 Promise 验证旧结果不能覆盖当前上下文，尤其是 Profile/Organization 切换、并行确认和 SFTP 重连。终端需覆盖缓冲达到上限、连续相同输出及一次输出超出保留窗口；SSH 建连需用本地服务器分别阻塞 channel、PTY、Shell，验证取消和超时均能退出，而活动会话不受建连 deadline 影响。

ZMODEM 修改需验证全字节二进制数据、分片握手和 UTF-8、双端真实协议收发、原生选择取消、断连后迟到的选择结果、下载文件名边界和同名文件保护、分块输出确认与关闭解锁。`go test -race ./internal/application/sshsession ./internal/application/zmodemfiles ./internal/sshclient` 检查核心并发边界；前端测试不使用真实账号或生产文件。用户本机还需使用实际 `lrzsz` 和 JumpServer 验证策略兼容性。

下载缓冲还需覆盖恰好 50 MiB、超过额度后的分批写入、多会话共享额度、空文件、取消清理和 Flush 失败。进度需覆盖原地刷新与节流、保存完成前不显示 100%、Shell 提示符恢复、多文件顺序、取消后的迟到回调和完整路径控制字符过滤；验证上传提示包含本机完整路径但协议仅发送文件名，下载提示使用实际保存路径及同名另存后缀。

CLI 传输运行 `go test -race ./internal/zmodem ./internal/clitransfer ./internal/sshclient`，验证默认启用、可选目录覆盖、系统目录不可用回退、本地路径隔离和取消后输入恢复。前端执行过 `npm ci` 且 Node.js 可用时，测试自动启动独立的 `zmodem.js` 对端验证互通；缺少这些测试依赖会明确 skip，不影响 CLI 构建或运行。PATH 中存在真实 `rz` / `sz` 时运行 lrzsz 互通；Windows 可用 `JUMPACCESS_TEST_LRZSZ_WSL_DIR` 指向 Ubuntu-22.04 中的测试二进制目录，使用 WSL 进行互通验证。macOS 默认目录的原生实现仍需 macOS smoke test。

生产行为采用 RED–GREEN–REFACTOR：先添加能够说明行为的失败测试，确认失败原因正确，再实现最小改动并重构。

默认测试不需要真实 JumpServer 账号，优先覆盖：

- TOML、Profile 和 Alias 解析。
- OAuth PKCE、原生 `jms` callback、JumpServer 确认页 URL、Token 过期、轮换和并发刷新。
- 使用本地 HTTP server 模拟 JumpServer API。
- 使用本地 SSH client/server 验证直接模式和 ProxyCommand 的协议边界。
- stdout、stderr、退出码以及敏感信息脱敏。
- Token 刷新失败不会关闭活动 SSH Session。
- React 界面通过可注入的类型化后端测试分页、Alias 搜索与账号选择、设置持久化、Tab 状态机与工作区恢复、SSH 断连/重连/输出事件和主机密钥确认。

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
- Windows 窗口使用 Wails `Frameless` 并保留 DWM 装饰，由 React 渲染标题栏和最小化/最大化/关闭按钮；只有标题栏空白区标记为可拖动。最大化按钮通过 Wails 窗口状态在最大化与还原图标之间同步切换，并在窗口 resize、重新获得焦点及按钮操作后校正状态。
- Wails 2.14 的前端边缘缩放检测未判断最大化状态；同步窗口状态时需更新内部 `window.wails.flags.enableResize`，最大化时清除 `resizeEdge` 并恢复原光标，还原时重新启用边缘缩放。升级 Wails 时需重新核对这些内部字段及运行时行为。
- Wails 2 的 HWND-hosted WebView 不原生支持把 HTML 自绘按钮注册成 Windows caption button，因此当前不实现最大化按钮悬停 Snap Layout；用户仍可使用 Windows 的其他窗口布局入口。待 Wails 3 正式版提供稳定的 composition hosting 与 non-client region 支持后再评估接入，不在 Wails 2 上维护原生覆盖窗口方案。
- macOS 使用 `TitleBarHiddenInset` 并保留左侧原生 traffic lights，前端不渲染右侧窗口控制按钮。
- Windows 通过 `EnumDisplayMonitors` 与显示器工作区恢复窗口；macOS 通过 AppKit `NSScreen` 恢复窗口。不得把 Wails 在不同平台返回的窗口坐标直接当作统一的虚拟桌面绝对坐标持久化。
- 终端字体候选在 Windows 通过 GDI 枚举并按 fixed-pitch 指标筛选，在 macOS 通过 CoreText family 与 monospace trait 筛选；其他目标或原生接口失败时返回空候选，由前端保留 `monospace` 和手工输入能力。

## 许可证与发布物

- JumpAccess 自身采用根目录 `LICENSE` 中的 MIT License；README 只使用链接到该文件的许可证徽章，不另设重复章节。
- 当前 Windows 和 macOS 生产依赖使用 MIT、ISC、Apache-2.0、BSD-2-Clause 或 BSD-3-Clause，没有 GPL、AGPL、LGPL 等 copyleft 依赖。依赖版本、版权声明和完整条款汇总在 `THIRD-PARTY-NOTICES.txt`。
- `LICENSE` 和 `THIRD-PARTY-NOTICES.txt` 通过 Go `embed` 编译进 `jumpctl`，`jumpctl licenses` 必须在单个可执行文件中保持可用；普通 `go build` 不需要复制额外文件即可保留可读声明。
- 正式 ZIP、tar 或安装包仍应把两份文本作为独立文件一并分发，方便不执行程序的接收者阅读。嵌入是单文件分发的保障，不替代正式归档中的显式材料。
- 新增或升级生产依赖时，必须重新检查目标平台的实际 package graph，更新第三方声明，并验证 `jumpctl licenses`。只用于测试、文档生成且不进入发布二进制的模块不需要混入发布声明。

## 自动发布

`.github/workflows/release.yml` 监听 `v*.*.*` 标签，并进一步拒绝不符合 `vX.Y.Z` 或 `vX.Y.Z-prerelease` 的标签。标签指向的提交必须已经包含该工作流。发布顺序为：

1. 在 Windows runner 上运行 `go test ./...`、`go vet ./...`、前端测试、前端生产构建和发布脚本测试。
2. 在 Windows amd64 runner 上构建 `jumpctl.exe` 与 Wails `jumpaccess.exe`，分别连同许可证文件压缩为 ZIP；不生成 NSIS 安装程序。
3. 在 Intel 与 Apple Silicon macOS runner 上原生构建对应架构的 `jumpctl`，保持 CGO 与 Keychain 能力，并使用 tar.gz 保留可执行权限。
4. 在 Apple Silicon macOS runner 上使用 Wails `darwin/universal` 构建同时包含 x86_64、arm64 的 `JumpAccess.app`，再连同许可证文件压缩为 ZIP。
5. 汇总五个归档、生成 `checksums.txt`，根据上一个版本标签到当前标签之间的 Conventional Commits 生成分类 Release Notes，最后创建 GitHub Release。任一前置 Job 失败都不会发布 Release。

源码中的 CLI/GUI `version` 保持为 `dev`，`wails.json` 的 `info.productVersion` 只作为开发占位值。发布工作流从标签移除前导 `v`，通过 `-ldflags "-X main.version=<version>"` 注入程序版本，并调用 `scripts/set-wails-version.mjs` 在 runner 临时工作副本中写入仅含 `X.Y.Z` 的平台元数据版本。该临时改动不会提交或推回仓库。

Release Notes 由 `scripts/release-notes.mjs` 生成，识别 `feat`、`fix`、`perf`、`refactor`、`docs`、`build`、`ci`、`test`、`chore` 以及 `!`/`BREAKING CHANGE`。提交说明应继续使用清晰的 Conventional Commit 格式；首个版本链接到完整提交历史，后续版本链接到 GitHub 标签比较页。

GitHub 托管 runner 已提供发布步骤使用的 `gh`，本机不要求安装 GitHub CLI。工作流只给最终 `publish` Job 配置 `contents: write`，并使用 GitHub 自动生成的短期 `GITHUB_TOKEN`。通常不需要保存额外 Secret；如果仓库或 Organization 策略禁止 Actions 写入内容，需要在 GitHub 的 `Settings → Actions → General → Workflow permissions` 中允许工作流写入。代码签名与 macOS notarization 尚未配置，后续启用时应通过 GitHub Actions Secrets 提供证书和凭据，不能提交到仓库。

## 当前验证入口

```powershell
go test ./...
go vet ./...
go build -trimpath ./cmd/jumpctl
cd cmd/jumpaccess
npm ci
npm test
npm run build
wails build
node --test ../../scripts/*.test.mjs
```

Windows 发布或人工验收构建不得使用 `wails build -nopackage`：Wails 2.14 会因此跳过平台资源生成，裸 EXE 不包含应用图标和版本资源。更新 `appicon.svg` 后应重新渲染 `appicon.png`，移除旧的 `build/windows/icon.ico` 并执行一次 `wails build`，由 Wails 重新生成 256、128、64、48、32 和 16 像素的 Windows 图标。

每次代码修改结束前，运行与本次修改最相关的测试；在交付完整阶段前运行全量测试、静态检查和目标平台构建检查。只有真实执行成功的命令才能写成当前可用入口。
