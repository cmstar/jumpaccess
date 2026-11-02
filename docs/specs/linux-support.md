# Linux CLI 与 MCP 支持设计

## 状态

- 文档状态：Implemented；Ubuntu 24.04 LTS / WSL2 amd64 完整回归、平台 race、静态检查、真实 JumpServer CLI/MCP 验收及 amd64/arm64 试打包已验证；GitHub 原生 CI 与 Go 1.25.x 运行结果仍待确认。本机验证使用 Go 1.27.1，详见 [7.3 验收结果](#73-已完成的验收与待确认项)
- 需求范围：已确认
- 设计评审：已确认
- 基线版本：`v0.2.0-pre.4`（`429affdafcf83498c2a6c444702fbcbe0e1175ed`）
- 目标平台：Ubuntu 24.04 LTS 与运行相同发行版的 WSL2，`linux/amd64` 与 `linux/arm64`；Ubuntu 22.04 LTS 预期兼容
- 更新时间：2026-11-02

## 1. 背景

JumpAccess 当前面向 Windows 与 macOS 提供 CLI、GUI 和 MCP 能力。目标用户在 Windows 主机中通过 WSL2 运行 Codex，Cursor 则可能运行在 Windows 或 WSL2。若 MCP Server 只运行在 Windows，WSL2 NAT、回环地址、进程生命周期和凭据路径会增加接入复杂度；提供原生 Linux CLI/MCP 可以让 WSL2 内的 Codex 直接通过本机回环地址访问 JumpAccess。

在 `v0.2.0-pre.4` 上执行 Linux 基线测试时，平台无关包可以编译，但完整 `go test -count=1 ./...` 因缺少 Linux 实现而失败。已确认的直接缺口包括：

- `internal/appdir` 仅接受 Windows 与 macOS；
- `internal/credential` 缺少 Linux 私密文件校验、加固、替换与目录同步实现；
- `internal/filelock` 缺少 Linux 文件锁实现；
- `internal/systemopen` 缺少 Linux 浏览器打开实现；
- `jumpctl` 的 ProxyCommand 稳定主机密钥只支持 Windows Credential Manager 与 macOS Keychain；
- CI 与 Release workflow 未覆盖 Linux。

现有 SSH、SFTP 下载、JumpServer Client、MCP HTTP Server、CLI command 和大部分配置逻辑不依赖桌面平台，可以复用。

## 2. 目标

首期交付以下 Linux 能力：

1. 在 Ubuntu 24.04 LTS 与运行相同发行版的 WSL2 上正式支持 `linux/amd64`、`linux/arm64` 的 `jumpctl`；Ubuntu 22.04 LTS 保持预期兼容。
2. 支持现有 Profile、Alias、手工 OAuth 登录、资源查询、交互式 SSH、ProxyCommand、MCP Server 与文件下载能力。
3. Codex 或 Cursor 在同一 WSL/Linux 环境中可通过默认回环地址接入 MCP Server。
4. 在无桌面 Keyring 的 headless 环境安全保存 OAuth Token 与 ProxyCommand 稳定主机密钥。
5. 增加 Linux amd64/arm64 原生 CI 验证和两种架构的 CLI Release 产物。
6. 保持现有 Windows 与 macOS 行为兼容。

## 3. 非目标

首期明确不包含：

- Linux Wails GUI 的正式支持或发布；
- AppImage、Deb、RPM、Snap、Flatpak 等安装包；
- `linux/amd64`、`linux/arm64` 之外的架构，Ubuntu 20.04 LTS 及更早版本，或支持矩阵未列出的 Ubuntu 与其他 Linux 发行版的兼容承诺；
- Secret Service、GNOME Keyring、KWallet 等桌面凭据后端；
- Linux 上的 `jms://` 自定义协议注册；
- systemd service、开机自启、Docker 镜像或公网 MCP 暴露；
- JumpServer API、MCP tool schema、CLI command 语义或下载授权模型的变更；
- 在 Windows 与 WSL2 之间共享同一份运行时目录、Token 或锁文件。

## 4. 支持矩阵

平台支持级别：

| 平台 | amd64 | arm64 | 支持级别 |
| --- | --- | --- | --- |
| Ubuntu 24.04 LTS 原生环境 | 支持 | 支持 | 正式支持范围；CI 已配置两个架构的原生 runner，本轮尚待确认运行结果 |
| Ubuntu 24.04 LTS on WSL2 | 支持 | 支持 | 正式支持范围；Windows/WSL 特有边界通过手工 smoke test 验证 |
| Ubuntu 22.04 LTS 原生环境或 WSL2 | 预期兼容 | 预期兼容 | 尚未纳入持续验证，不作为正式支持基线 |
| Ubuntu 20.04 LTS 及更早版本 | 不承诺 | 不承诺 | 可能可以运行，但不提供正式支持或兼容承诺 |
| 其他或未列出的 Ubuntu LTS | 未验证 | 未验证 | 验证并更新本矩阵后才进入支持范围 |
| 其他 Linux 发行版 | 未验证 | 未验证 | 不提供兼容承诺 |

“预期兼容”只表示当前 `CGO_ENABLED=0` 静态产物没有已知的发行版动态库依赖，不等同于正式支持、持续测试或缺陷修复承诺。新发布的 Ubuntu LTS 也不因名称自动进入正式支持范围。

在正式支持的平台上，CLI 能力矩阵如下：

| 能力 | amd64 | arm64 | WSL2 对应架构 | 说明 |
| --- | --- | --- | --- | --- |
| `jumpctl` CLI | 支持 | 支持 | 支持 | 与现有命令保持兼容 |
| Profile / Alias | 支持 | 支持 | 支持 | 使用 Linux 应用目录 |
| OAuth 手工登录 | 支持 | 支持 | 支持 | 浏览器打开失败时复制 URL，或使用 `--no-browser` |
| 资源查询 | 支持 | 支持 | 支持 | 复用现有 JumpServer Client |
| 交互式 SSH | 支持 | 支持 | 支持 | 复用终端能力 |
| ProxyCommand | 支持 | 支持 | 支持 | 稳定主机密钥采用严格私密文件存储 |
| MCP HTTP Server | 支持 | 支持 | 支持 | 默认仅监听回环地址 |
| 文件下载 | 支持 | 支持 | 支持 | 默认下载目录为 `~/Downloads` |
| Wails GUI | 不支持 | 不支持 | 不支持 | 不作为 Linux 产品承诺 |

## 5. 设计方案

### 5.1 Linux 应用目录

Linux 应用根目录使用：

1. 若 `XDG_CONFIG_HOME` 已设置，使用 `$XDG_CONFIG_HOME/JumpAccess`；
2. 否则使用 `~/.config/JumpAccess`。

配置、OAuth Token、日志、锁和 CLI/MCP 运行数据继续位于同一应用根目录下的既有子目录，以减少跨平台结构差异。Linux 不读取 Windows `%APPDATA%` 中的数据，也不自动迁移或链接 Windows 目录。

应用根目录及私密子目录必须位于能够可靠执行 Unix ownership 与 permission 校验的文件系统。若目录位于无法落实这些约束的 DrvFS、网络盘或其他文件系统，凭据初始化必须 fail closed，并给出迁移至 WSL Linux 文件系统的提示。

### 5.2 OAuth Token 私密文件

Linux 复用 macOS 私密文件后端的安全模型，并提供独立的 Linux platform 文件：

- 私密目录权限必须为 `0700`；
- 凭据文件必须是 regular file，权限必须为 `0600`；
- 文件 owner 必须是当前 effective user；
- 拒绝 symlink、宽松权限、错误 owner 和非 regular file；
- 写入使用同目录临时文件、`fsync`、atomic rename 和目录同步；
- 不在日志、错误、测试 fixture 或文档中输出真实 Token。

发现不安全的既有路径时不静默放宽校验。仅在路径 owner 正确且目标类型明确时修复 permission；其他异常必须停止并提示用户处理。

### 5.3 ProxyCommand 稳定主机密钥

现有 Windows Credential Manager 和 macOS Keychain 行为保持不变。Linux 不引入桌面 Keyring 依赖，而是在应用根目录下使用 `ssh/proxy_host_ed25519_key` 保存 ProxyCommand façade 的稳定主机私钥。`ssh` 目录权限为 `0700`，私钥权限为 `0600`。

实现上为原生凭据工厂增加可显式传入应用根目录的 Linux 分支，底层复用 5.2 的严格私密文件机制。密钥生成、序列化格式与 SSH host-key trust 行为保持不变。

### 5.4 文件锁

Linux 使用 `golang.org/x/sys/unix.Flock` 实现 non-blocking exclusive lock 与 unlock，语义与现有 Darwin 实现一致。锁竞争继续返回现有领域错误，不引入轮询或后台协调器。

### 5.5 浏览器打开

Linux 使用 `xdg-open <url>` 打开 OAuth 授权页面：

- `xdg-open` 不存在、无可用桌面会话或启动失败时，返回可操作错误并保留授权 URL；
- headless/CI/SSH 场景使用现有 `--no-browser` 路径，由用户在可信浏览器中打开 URL；
- 不执行 shell 拼接，URL 作为独立 process argument 传递。

WSLg 可用时属于 `xdg-open` 的环境能力，不作为 JumpAccess 的硬依赖。

### 5.6 SSH 与 ProxyCommand

交互式 SSH 与 ProxyCommand 复用现有实现。Linux 平台必须维持以下边界：

- ProxyCommand 的 stdout 只承载 SSH 字节流，诊断信息写 stderr；
- JumpServer resource host key 与本地 façade host key 仍是两层独立 trust boundary；
- 首次信任、known-host 更新与 mismatch 处理不得因 Linux 支持而放宽；
- 不新增 shell command 拼接或隐式远程命令执行入口。

### 5.7 MCP 与下载

Linux MCP Server 复用现有 HTTP transport、认证上下文和 tool schema：

- 默认只监听 `127.0.0.1`；
- Codex 与 Cursor 应优先在 MCP Server 所在的同一 Linux/WSL2 环境接入；
- 不为跨 Windows/WSL 暴露而自动监听 `0.0.0.0`；
- OAuth Token 不写入 MCP 客户端配置；客户端配置只保存本地 server 地址及非敏感启动参数；
- 文件下载沿用既有授权与 SFTP 流程，Linux 默认目录为 `~/Downloads`；
- 不自动把下载写入 `/mnt/c`，需要时由用户显式选择目标路径。

### 5.8 CI

新增 Ubuntu 24.04 LTS amd64/arm64 原生 job；当前 runner 分别为 `ubuntu-24.04` 与 `ubuntu-24.04-arm`，至少执行：

```bash
go test -count=1 ./...
go test -race ./internal/credential ./internal/filelock ./internal/appdir ./internal/systemopen
go vet ./...
go build ./cmd/jumpctl
python3 -B internal/application/agentmcp/download_remote_test.py
```

CI 使用 `CGO_ENABLED=0` 验证共享 Go test graph 与 CLI，不构建或发布 Linux Wails GUI。

新增 Linux 定向测试，覆盖：

- XDG 与默认应用目录选择；
- 私密目录/文件 permission、owner、symlink、atomic replace 与目录同步；
- lock acquire、竞争、释放和进程退出后重获；
- `xdg-open` 成功、缺失与失败；
- Linux 原生凭据工厂及稳定主机密钥持久化；
- CLI/MCP 启动 smoke test。

### 5.9 Release

首期在 tag Release 中新增 `linux/amd64` 与 `linux/arm64` CLI 压缩包：

- 分别使用 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 和 `GOARCH=arm64` 构建 `jumpctl`；
- 产物名为 `jumpctl-v<version>-linux-amd64.tar.gz` 与 `jumpctl-v<version>-linux-arm64.tar.gz`；
- 压缩包包含 `jumpctl`、License 与必要 notice；
- 生成并发布 checksum；
- 不包含 Linux GUI artifact。

现有 Windows 与 macOS release 产物、签名和命名保持不变。

## 6. 兼容性与安全边界

- 不修改 JumpServer server-side 配置和 API contract。
- 不更改 MCP tool 名称、参数和返回结构。
- 不迁移或共享 Windows/macOS 凭据；各 OS 独立保存。
- MCP 默认回环监听，远程访问仍需用户明确配置网络与外围认证。
- Linux 文件凭据的安全性依赖本机 Unix account 和 filesystem permission；不宣称具备 hardware-backed 或 desktop-keyring 等级的保护。
- 真实 JumpServer 验证只使用非生产或明确批准的低权限账号，命令执行与下载目标必须由人工确认。
- 仓库、CI log、测试数据和文档不得包含真实 Token、私钥、Cookie 或下载内容。

## 7. 验收方案

### 7.1 自动验证

在 Ubuntu 24.04 LTS amd64/arm64 原生 runner 完成：

1. 完整 Go tests、定向 race tests、`go vet` 与 CLI build 全部通过；
2. MCP download Python test 通过；
3. CI workflow 与 Release workflow 的结构测试通过；
4. Windows 与 macOS 现有 tests 不回退；
5. Release dry-run 能生成预期 Linux CLI archive 与 checksum，且不包含 credential。

### 7.2 本机 smoke test

正式验收在 Ubuntu 24.04 LTS 原生环境或运行相同发行版的 WSL2 执行；Ubuntu 22.04 LTS 可使用同一清单做兼容性 smoke，但结果不改变其支持级别：

1. 新建隔离的测试应用目录并检查目录/文件 permission；
2. 使用 `--no-browser` 完成手工 OAuth 登录；
3. 查询授权范围内的节点与资产；
4. 启动 MCP Server，并分别用 Codex 与 Cursor 的本地客户端完成 initialize、资源查询；
5. 对批准的非生产资产执行无副作用命令；
6. 下载批准的非敏感测试文件并校验目标路径与内容；
7. 验证进程退出、锁释放、Token 重载及日志脱敏。

真实环境 smoke test 不纳入自动 CI，也不在仓库保存 endpoint、username 或 Token。

### 7.3 已完成的验收与待确认项

截至 2026-11-02，本轮实际环境为 Ubuntu 24.04 LTS / WSL2 amd64，使用 Go 1.27.1；测试目标和文件均经使用者指定。以下只记录范围和结果，不包含真实站点、资产、账号、回调、凭据或业务文件内容。

| 范围 | 验收结果 |
| --- | --- |
| 本地自动化 | 完整 Go 回归、平台 race、`go vet`、CI/发布脚本测试和 Python 下载脚本测试通过；安装 `lrzsz` 后 SFTP → `sz` 回退集成测试通过 |
| 双架构试打包 | amd64/arm64 静态 ELF 与 tar.gz 内容、权限和校验和验证通过；仅 amd64 在本机实际运行，arm64 交叉构建不等于原生运行 |
| CLI 真实链路 | 手工 OAuth 登录、资源与账号查询、交互 SSH、重复连接、ProxyCommand 交互/exec 和 CLI ZMODEM 下载通过 |
| MCP 认证和查询 | 无凭据请求返回 401，协议初始化、工具发现和只读查询通过；Access Token 过期后资源查询自动续期，无需交互登录 |
| MCP 执行与控制 | Codex 原生工具调用的 stdout/stderr 分流、非零退出码、请求键去重、远端超时终止、主动取消及取消后连接复用通过 |
| MCP 下载 | 经 Codex 原生工具调用，SFTP 本地交付完成且 `files_ready=true`；去重、准备中取消确认、取消已完成任务仍保留最终文件通过；未向模型输出文件正文 |
| 服务生命周期 | 重复启动被拒绝；无活动任务时正常停止后端口与锁释放；重启后原生查询及新 SSH 执行成功，旧连接/执行/下载 ID 返回 `INSTANCE_CHANGED` |
| Agent 客户端 | Codex 当前会话原生调用通过；Cursor CLI 的 7 项只读工具发现通过，Cursor WSL GUI 的原生组织查询由使用者操作并提供成功结果 |
| 验收收尾 | 测试连接关闭，活动及未知任务计数为 0；临时开放执行/下载工具后恢复客户端的 7 项只读边界 |

客户端接入使用部署侧 stdio → HTTP 适配器读取项目原生 Token；该适配器及工具白名单不属于 `jumpctl` 发布物。以上客户端结果不代表 Cursor GUI 的命令执行/下载、Wails 桌面宿主或其他平台已完成同等验收。通用接入步骤见 [Linux/WSL2 MCP 指南](../manual/agent-mcp.md#linuxwsl2-接入-codex-与-cursor)。

仍需确认 Ubuntu 24.04 LTS amd64/arm64 GitHub 原生 CI、Windows/macOS 回归及正式 CI 工具链 Go 1.25.x 的结果；macOS 真实环境验收仍未覆盖。Ubuntu 22.04 LTS 维持预期兼容，未因本轮结果升级为正式支持。MCP 下载的链接交付、其他协议及更广边界继续以各自测试证据为准。

## 8. 发布与回滚

采用独立 Linux artifact 增量发布，不修改已有平台安装路径。发布前保留上一版本 checksum 与 artifact。

发生 Linux 专属问题时：

1. 停止分发或从 Release 标记中撤下 Linux artifact；
2. 用户停止本地 MCP/CLI 进程并恢复上一可用版本；
3. 保留应用目录以便回退读取兼容配置；若问题涉及 credential 格式，则先备份并按专项迁移方案处理，不自动删除；
4. Windows 与 macOS artifact 不受 Linux 回滚影响。

首期不进行配置 schema 变更，因此正常情况下二进制回退即可完成恢复。

## 9. 设计取舍

### 9.1 CLI/MCP 优先，GUI 延后

目标场景首先是 Codex 与 Cursor 的 JumpServer 能力接入。CLI/MCP 所需平台补齐较小，也避免把 GTK/WebKit 打包与桌面集成风险带入首期。

### 9.2 严格文件存储优先于 Secret Service

WSL2 与 headless Linux 不保证存在 D-Bus 或 Keyring。严格私密文件可以复用现有 OAuth 机制并保持可预测性，代价是安全级别受 Unix account 与 filesystem permission 限制。未来可在不改变上层 contract 的前提下增加可选 Keyring backend。

### 9.3 单一应用根目录

沿用现有项目结构比立即拆分 XDG config/data/state 目录更兼容，也减少迁移风险。若后续正式支持更多 Ubuntu LTS 或其他 Linux 发行版，可单独设计 XDG 细分迁移。

## 10. 实施结论

1. Linux 交付不重写 CLI 或 MCP 业务逻辑，由单一 `jumpctl` 二进制同时提供现有 CLI 与 MCP Server。
2. Linux amd64/arm64 已纳入 Ubuntu 24.04 LTS 原生 CI 与 tag Release，分别生成对应架构的 `jumpctl` tar.gz，不发布 Linux GUI；Ubuntu 22.04 LTS 仅保持预期兼容，其他版本须验证后再更新支持矩阵。
3. 本机自动测试及已授权的真实 JumpServer、Codex 原生调用和 Cursor 只读接入已完成，范围见 7.3；GitHub 原生 CI 结果与其他平台验收继续独立确认，不在仓库保存 endpoint、username 或 Token。
