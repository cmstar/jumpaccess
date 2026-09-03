# 开发说明

## 当前阶段

Go 工程、CLI 配置能力、OAuth Token 生命周期、JumpServer 连接准备协议、直接 SSH 和通用 ProxyCommand 已经建立；Wails GUI 已接通类型化资源 API、手工 OAuth 回调、进程生命周期内自动续期、Profile/Organization、分页资产与行内 Alias 管理、GUI 偏好、可持久化 Tab 工作区、无标准标题栏窗口与多 xterm SSH 会话。真实 JumpServer 和 macOS 原生环境仍待 smoke test；实际状态以测试和当前命令帮助为准。

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
internal/cli/       # CLI 参数和输出适配
internal/config/    # TOML 模型、校验和存储
internal/guiconfig/ # GUI 独有偏好与 gui.toml 存储
internal/credential/# 私有文件凭据与原生凭据兼容适配
internal/filelock/  # 多进程 Token 刷新锁
internal/jumpserver/# JumpServer REST 与 client-url 协议客户端
internal/oauth/     # OAuth Discovery、PKCE、callback 与 Token 协议
internal/proxyconsole/ # Windows ProxyCommand 私有控制台脱离适配
internal/sshclient/ # 直接 SSH 客户端会话
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
- Profile 范围内保存 Alias。修改配置时应支持用户直接批量编辑，并提供打开配置文件的快捷命令。
- 读取配置与构造外部客户端应显式发生在应用启动流程中，避免包初始化因缺少本机配置而失败。
- Wails 生成 bindings 时会编译并执行带 `bindings` build tag 的临时程序；该模式只构造用于类型反射的桌面适配器，不得解析应用目录、读取用户配置或凭据、创建外部客户端。严格配置校验只属于真实应用启动流程，构建结果不能依赖构建机上的 JumpAccess 用户数据。
- 桌面程序在 Wails 窗口初始化前发生启动错误时，必须同时写入 `stderr` 并通过 Windows/macOS 原生错误对话框告知用户；bindings 临时程序不得显示该业务对话框。

共享配置文件名为 `config.toml`。当前 schema 版本为 `1`；Profile 保存在 `[profiles.<name>]`，Alias 保存在 `[profiles.<name>.aliases.<alias>]`。GUI 独有偏好保存在同目录的 `gui.toml`，当前 schema 版本为 `3`：`[appearance]` 只保存应用主题，`[terminal]` 保存终端字体、字号和右键行为，`[tabs]` 保存 Tab 关闭按钮显示和关闭活动会话确认，此外还包含窗口最大化/普通边界、Tab 顺序、活动项和 SSH 重连描述符；CLI 不读取该文件。读取 schema v1/v2 时保留原有主题、终端字体、字号、Tab 偏好、工作区和窗口位置并迁移为 v3；v1 在 Windows 保存的是虚拟桌面绝对坐标，在 macOS 保存的是当前显示器相对坐标，读取时仍保留原值，由窗口恢复逻辑按平台解释。终端输出、live session ID 与运行状态不得写入 `gui.toml`。最大化或最小化退出时不得用临时窗口边界覆盖已保存的普通窗口边界。GUI 偏好和工作区写入也必须串行化 read-modify-write，防止并发保存互相覆盖。

默认每 30 秒检查一次 Token，并在过期前 1 分钟刷新；凭据更新使用同目录临时文件和原子替换。`jumpctl ssh`、`jumpctl proxy` 和桌面 GUI 使用独立的刷新监督器；GUI 监督器覆盖所有保存了 Refresh Token 的 Profile，每轮重新读取配置和凭据以发现运行期间的变化，并在桌面程序退出时取消。刷新失败会报告告警，但监督器不拥有也不取消活动 SSH Session。SSH gateway 信任记录位于同一应用根目录的 `known_hosts`。

## CLI 与进程 I/O

- 普通交互命令可以使用 stdout/stderr 与用户沟通。
- `jumpctl proxy` 的 stdout 专用于 SSH 协议数据；日志、诊断和可操作错误只写 stderr。
- Proxy 模式不得启动浏览器或请求交互选择。认证或目标解析失败时返回明确的非零退出码。
- Windows 的 `proxy` 在连接准备和本地 SSH façade 握手期间保留 stderr；握手成功后，只有 stdin、stdout 均不是 Console 句柄且当前控制台仅附着 `jumpctl` 一个进程，才以 best-effort 调用 `FreeConsole`。共享终端、交互句柄或任何检查失败时保持附着；非 Windows 平台不执行该优化。
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
- Windows 窗口使用 Wails `Frameless` 并保留 DWM 装饰，由 React 渲染标题栏和最小化/最大化/关闭按钮；只有标题栏空白区标记为可拖动。
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
