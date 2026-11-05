# Linux CLI 支持设计

## 状态

- 范围：基于上游 `master`，仅增加 Linux CLI 平台适配、验证与发布归档。
- 基线提交：`8dff7b0e02298aba1cd47b02324de32cfe5294da`。
- 实施：平台适配已完成；实现提交 `864ab62` 的五项原生 CI、本机自动化及本轮约定的 WSL2 amd64 真实环境 CLI 验收通过，包括全新授权及 ZMODEM 上传回传。旧分支的验收不替代本分支验证。

## 1. 背景与目标

Go 核心业务可跨平台复用，但上游缺少 Linux 应用目录、文件锁、凭据权限及系统打开适配，导致 CLI 无法编译。补齐这些平台边界，交付 amd64、arm64 的 `jumpctl`，覆盖现有 Profile、Alias、OAuth、资源查询、交互 SSH、ZMODEM 和通用 ProxyCommand。

## 2. 范围

支持无桌面环境，沿用 `auth login --no-browser` 的手工授权流程。桌面环境使用 `xdg-open` 打开浏览器或配置文件；无可用浏览器时由用户在可信设备完成授权并粘贴回调。

Linux 检查 `xdg-open` 的退出结果并回收进程，启动器不存在或非零退出均返回错误。登录在后台观察启动器，同时接收 OAuth 回调，不要求浏览器先退出；等待回调期间检测到打开失败时提示使用 `--no-browser` 重试。`config edit` 等待启动器退出，若默认编辑器以前台方式运行，会等待编辑器关闭。Windows/macOS 保留异步打开方式并回收启动器进程。

不包含 Linux Wails GUI、桌面 Keyring、自定义协议注册、安装器、自启服务、配置迁移或依赖升级。未合入上游 `master` 的 Agent/MCP 功能不在本变更内。

## 3. 平台适配与安全边界

- 应用根目录：`$XDG_CONFIG_HOME/JumpAccess`，未设置时为 `~/.config/JumpAccess`；使用 Go `os.UserConfigDir()` 解析。
- OAuth：沿用应用根目录 `credentials` 中每个 Profile 一个文件及现有摘要文件名。
- ProxyCommand：稳定主机私钥位于应用根目录的 `ssh/proxy_host_ed25519_key`；首次生成后持续复用，与 OAuth Token 及上游 `known_hosts` 分离。
- 主机私钥的读取、首次生成和保存整体使用应用根目录 `locks/ssh-proxy-host-key.lock` 的跨进程锁；同时首次连接的进程复用同一把密钥。等待锁遵循连接上下文，最长 10 秒；密钥损坏时返回错误，不自动重建。
- 私密目录为当前用户所有的 `0700`，文件为 `0600`；读取拒绝符号链接、非普通文件、错误所有者和不符权限。写入使用同目录临时文件、刷盘、原子替换和目录同步。
- 文件锁复用现有锁接口，以 `unix.Flock` 实现竞争与释放；不改变上层配置更新和 Token 生命周期规则。
- Windows Credential Manager 与 macOS Keychain 行为保持原样。
- Windows 与 WSL 各自保存配置、Token、密钥与锁，不共享运行目录。WSL 应使用 Linux 文件系统保存私密文件；不满足权限约束时拒绝使用。

## 4. 支持矩阵

产品支持 Ubuntu LTS，当前承诺范围由以下验证矩阵限定：

| 平台 | 架构 | 支持级别 |
| --- | --- | --- |
| Ubuntu 24.04 LTS 原生环境 | amd64、arm64 | 持续验证基线，新增原生 CI；合并前须确认新分支结果 |
| Ubuntu 24.04 LTS / WSL2 | 对应架构 | 使用同一 Linux CLI；WSL 特有边界需手工验收 |
| Ubuntu 22.04 LTS | amd64、arm64 | 预期兼容，未纳入持续验证 |
| Ubuntu 20.04 LTS 及更早版本 | — | 不在正式支持范围 |
| 未列出的 LTS 或其他发行版 | — | 未验证，不自动获得支持承诺 |

`CGO_ENABLED=0` 产物不依赖发行版动态 C 库，但不代表任意内核、文件系统或发行版都已验证。arm64 交叉编译不能替代 arm64 原生运行。

## 5. 验证与发布

CI 在 `ubuntu-24.04` 和 `ubuntu-24.04-arm` 分别运行全量 Go 测试、平台 race、静态检查、CLI 构建与隔离配置目录下的帮助 smoke。Windows/macOS 继续使用原有 CLI/GUI 检查；共享能力与现有凭据后端不得回退。

Release 新增 `jumpctl-v<version>-linux-amd64.tar.gz` 和 `jumpctl-v<version>-linux-arm64.tar.gz`，使用关闭 CGO 的原生构建。每个归档包含可执行的 `jumpctl`、`LICENSE`、`THIRD-PARTY-NOTICES.txt`，进入现有 SHA-256 校验及发布门禁。

真实 JumpServer 验收需在经批准的测试目标上确认：手工 OAuth 登录和刷新、资源查询、交互 SSH、窗口调整、ProxyCommand 交互与 exec、重启后主机指纹稳定、ZMODEM 上传与下载。不得把真实凭据、站点、账号或业务文件写入测试和 PR。

### 本地验收进度（2026-11-04）

本轮使用实现提交 `864ab62` 构建的独立测试二进制，环境为 Ubuntu 24.04.3 LTS / WSL2 amd64、Go 1.27.1，未覆盖当前部署的 CLI/MCP 二进制。结果不扩展为 Ubuntu 22.04 LTS 或 Linux GUI 验收。

| 项目 | 结果 |
| --- | --- |
| 自动化与打包 | 全量 Go 回归、静态检查、平台 race、发布脚本及双架构试打包通过；本轮补充 SSH、ProxyCommand、主机信任、ZMODEM、OAuth 生命周期 race 回归通过 |
| 原生 CI | [五项检查通过](https://github.com/bingeger/jumpaccess/actions/runs/41778915255)，包括 Linux amd64/arm64 原生运行及 Windows/macOS CLI、GUI 构建；CI 使用 Go 1.25 工具链 |
| OAuth 续期与查询 | 既有 Access Token 过期后，新二进制查询成功并保存续期结果；新进程读取认证状态正常 |
| ProxyCommand 交互与 exec | 严格主机信任下只读命令成功；交互 Shell 的 stdin/stdout 均为终端；独立进程重复连接保持主机密钥稳定，stdout/stderr 分流及退出码 7 透传通过 |
| 交互 SSH 与窗口尺寸 | 本地真实 PTY 连接成功；80×24 调整到 132×42 后远端尺寸同步通过 |
| ZMODEM 下载 | 公开系统文件下载显示 Complete；本地权限 0600，与远端 SHA-256 一致；下载后 Shell 可用，正常退出 |
| 全新 OAuth 登录 | 使用者在独立配置目录完成 `--no-browser` 授权并提供 authenticated 结果；新二进制组织查询成功，未复制既有凭据 |
| ZMODEM 上传回传 | 使用者将 40 字节合成文件上传到远端临时目录并下载，两方向均显示 Complete；远端与本地 SHA-256 一致，本机再次执行 cmp 通过，下载权限 0600；使用者清理本次远端文件及临时目录 |

自动化 PTY 脚本的两次下载探测分别因等待进度文本超时、在文件创建但未写完时过早断言而未完成，均未计为通过；随后通过直接终端观察 Complete、核对 SHA-256 与退出状态完成下载验收。未将脚本未完成的尝试记为产品成功。

## 6. 回滚

本变更不修改配置 schema，不迁移其他平台数据。部署时保留旧二进制及校验值；停止 CLI 后可恢复旧版。保留配置与凭据，不自动删除主机私钥，否则外部 SSH 客户端将看到指纹变化。Linux 发布故障可单独停止分发 Linux 归档，不改动其他平台发布物。
