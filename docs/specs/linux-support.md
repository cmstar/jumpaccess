# Linux CLI 支持设计

## 状态

- 范围：基于上游 `master`，仅增加 Linux CLI 平台适配、验证与发布归档。
- 基线提交：`8dff7b0e02298aba1cd47b02324de32cfe5294da`。
- 实施：平台适配已完成；新分支的原生 CI 与真实环境验收待确认，旧分支的验收不替代本分支验证。

## 1. 背景与目标

Go 核心业务可跨平台复用，但上游缺少 Linux 应用目录、文件锁、凭据权限及系统打开适配，导致 CLI 无法编译。补齐这些平台边界，交付 amd64、arm64 的 `jumpctl`，覆盖现有 Profile、Alias、OAuth、资源查询、交互 SSH、ZMODEM 和通用 ProxyCommand。

## 2. 范围

支持无桌面环境，沿用 `auth login --no-browser` 的手工授权流程。桌面环境使用 `xdg-open` 打开浏览器或配置文件；无可用浏览器时由用户在可信设备完成授权并粘贴回调。

不包含 Linux Wails GUI、桌面 Keyring、自定义协议注册、安装器、自启服务、配置迁移或依赖升级。未合入上游 `master` 的 Agent/MCP 功能不在本变更内。

## 3. 平台适配与安全边界

- 应用根目录：`$XDG_CONFIG_HOME/JumpAccess`，未设置时为 `~/.config/JumpAccess`；使用 Go `os.UserConfigDir()` 解析。
- OAuth：沿用应用根目录 `credentials` 中每个 Profile 一个文件及现有摘要文件名。
- ProxyCommand：稳定主机私钥位于应用根目录的 `ssh/proxy_host_ed25519_key`；首次生成后持续复用，与 OAuth Token 及上游 `known_hosts` 分离。
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

## 6. 回滚

本变更不修改配置 schema，不迁移其他平台数据。部署时保留旧二进制及校验值；停止 CLI 后可恢复旧版。保留配置与凭据，不自动删除主机私钥，否则外部 SSH 客户端将看到指纹变化。Linux 发布故障可单独停止分发 Linux 归档，不改动其他平台发布物。
