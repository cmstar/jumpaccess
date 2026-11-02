# 使用 MCP Server

GUI 和 CLI 都可以启动同一套 Streamable HTTP MCP 服务，让 Agent 查询资源、复用 SSH 连接、执行命令及下载文件。默认服务地址为 `http://127.0.0.1:39287/mcp`，配置与访问 Token 共用 `config.toml`，同一应用数据目录只运行一个实例。先通过任一入口完成所需 Profile 的 JumpServer 登录，再选择 GUI 或 CLI 承载服务。

## 在 GUI 中使用

点击窗口顶部的 **MCP 插头图标**，打开独立 Agent MCP Tab，页面标题为 **MCP Server**。图标右下角的绿灯表示服务运行中，红灯表示未运行；悬停可查看状态。重复点击会回到同一 Tab；关闭 Tab 不停止服务。GUI 默认不自动启动 MCP，可在配置页开启自动启动；退出应用会停止服务。页头右侧说明哪个进程提供服务，区分当前桌面应用、其他桌面应用或命令行进程。

模块内按“日志、连接、配置”排列，已配置时默认进入日志页。启动、停止、重启分别用绿色、红色、琥珀色区分，不可用的按钮会淡化显示。

下方三张截图使用演示数据，Profile、目标与 Token 均为样例。

### 配置与启动

配置页上方是“启动行为”，下方是“连接与访问配置”。两张卡片左右对齐，沿用设置模块的宽度与留白方式：宽窗口下达到最大宽度后不再拉伸，较窄窗口下随可用空间缩放。

1. 先在 Profile 中完成 JumpServer 登录。
2. 首次使用、尚未保存 Bearer Token 时，MCP 自动进入“配置”页并显示引导。检查监听地址和端口，输入或生成 Bearer Token，然后保存；仅生成而未保存时仍不能启动。默认只允许本机访问；TLS 证书与私钥同时留空时使用 HTTP。
3. 点击公共区域的“启动”，把显示的实际服务地址和配置中的 Token 提供给 Agent。
4. “重置”和“保存”位于配置表单右上角；有修改时，“重置”左侧会出现琥珀色“未保存”标签，保存或重置后隐藏。修改配置后需要保存并重启服务才生效。“重置”只恢复上次保存值，运行中的地址不会随草稿变化。

![MCP 配置：两张卡片左右留白，连接配置已保存，随应用启动开关已开启，当前服务尚未启动](../screenshots/mcp-01-config.png)

图中已保存连接配置，因此没有“未保存”标签，表单的“重置”和“保存”按钮呈禁用状态。“随应用启动 MCP”是演示中手动开启的，默认仍为关闭；它只影响下次应用启动，所以当前服务仍显示“已停止”。本次要使用服务，点击上方“启动”。实际使用时请在自己的应用中生成 Token，图中的 `DEMO_ONLY` 值不能用于真实服务。

存在活动任务时，停止或重启前会显示确认。无法确认远端命令结束时，服务会提示结果未知；应先核对服务器状态，不能直接重跑。若服务由其他进程拥有，请在那个宿主中管理连接和日志。

### 随应用自动启动

“配置”页顶部的“启动行为”提供 **随应用启动 MCP** 开关，默认关闭。先完成并保存下方的连接与访问配置，才能开启。开关更改后立即自动保存到 GUI 专用的 `gui.toml`，不需要点击表单的“保存”；表单的“重置”也不会改变开关。

更改从下次启动桌面应用起生效，不会立即启动或停止当前 MCP 服务。自动启动在后台进行，不依赖 MCP Tab 是否打开，也不阻塞资产查询、终端等功能。失败时会显示非阻塞提示，并在 MCP 日志中保留记录；本次应用运行期间不会自动重试，可检查配置或端口占用后手动启动。

### 查看运行日志

“日志”页顶部的“输出级别”提供精简、标准和详细三个选项，右侧同一行显示“仅影响后续日志，已显示的内容保持不变”，工具栏右端是“暂停显示”和“清空显示”。提示不再单独占行，为下方日志保留更多空间。

日志采用只读文本流。精简显示服务、连接和异常；标准增加命令受理、结束与取消，以及 MCP 工具调用摘要，资源查询包含返回数量、分页状态和耗时；详细增加命令输入和查询条件。切换级别只影响后续日志，已有行保持原样，不补显示此前过滤的查询条件。命令的非零退出码仍是正常返回；日志不显示 stdout/stderr 或查询结果正文，Agent 通过任务接口读取仍保留的输出。

服务信息查询、连接／任务查询、重复请求返回原结果、重复关闭和取消已结束任务也会留下调用记录。SDK 在进入业务处理前拒绝的工具调用记录为 `MCP_REJECTED`，具体错误由 MCP 响应提供。这里记录的是工具调用与业务事件，不是每个 HTTP 请求或协议初始化消息的访问日志。

![MCP 详细日志：级别提示与按钮同排，下方显示资源查询摘要和条件、命令受理、输入与完成状态](../screenshots/mcp-02-logs.png)

图中先切换为“详细”，再启动服务接收样例日志。`test -f` 返回 `exit=1`，执行状态仍是 `completed`；这表示命令已正常返回，条件未成立，不属于 SSH 发送或结果接收失败。

点击“终端样式”可调整共用的字体、字号、行高与配色，同时影响 SSH 终端和 MCP 日志。应用明暗主题不覆盖终端配色。

向上滚动时停止跟随，点击“回到最新”恢复。暂停显示期间继续采集，恢复后追加；缓存有上限，较早内容可能回收。“清空显示”不会删除任务结果或去重记录。日志仅在当前宿主内存中保留，同一进程重启服务后仍可查看部分历史，退出应用后丢失。

### 管理连接

“连接”页显示逻辑连接、实际 SSH 状态、目标、账号及活动任务。**SSH 已释放**时逻辑 ID 仍然有效，下次执行可自动重连。空闲连接可直接关闭；有任务的连接确认后等待任务结束，再使 ID 失效。

![MCP 连接管理：空闲、执行中和 SSH 已释放的三类连接，以及所选连接详情](../screenshots/mcp-03-connections.png)

图中 `web` 空闲，`builder` 正在执行任务，`prod-db` 的 SSH 已释放。选择连接可查看下方详情，点击“查看日志”只显示该连接已接收的日志；筛选条右侧的 × 按钮可取消连接筛选。

完整行为及容量限制见 [MCP 设计](../specs/agent-mcp.md)。

## 在 CLI 中使用

### 首次配置与启动

已有 Profile 时，可以直接执行：

```text
jumpctl mcp config --rotate-token
jumpctl mcp serve
```

尚未配置 Profile 时，先按 [CLI 入门](cli-quick-start.md) 添加站点。`--rotate-token` 在首次使用时生成 Token，已经配置时替换旧值；命令保存成功后自动列出全部配置，包括明文 Token。把输出中的服务 URL 和 Token 配置到 Agent。`serve` 使用已经保存的配置前台运行，并持续显示 MCP 调用日志。

MCP 调用不会打开登录或主机密钥确认窗口。如果连接提示 JumpServer SSH gateway 尚未受信任，先通过普通 CLI 或 GUI SSH 连接核对并确认指纹，再重试 Agent 请求。

当前终端在服务停止前不会返回命令提示符。按 Ctrl+C 停止服务并释放后台资源，已输出的日志和终端滚动历史原样保留，不清屏；重新运行 `jumpctl mcp serve` 即可再次启动。

### 修改配置

查看配置和单项修改：

```text
jumpctl mcp config
jumpctl mcp config --port 39288
```

一次修改多项、设置已有 Token，以及配置或关闭 TLS：

```text
jumpctl mcp config --listen-host 0.0.0.0 --allow-lan=true --rotate-token
jumpctl mcp config --token <value>
jumpctl mcp config --tls-cert <cert-path> --tls-key <key-path>
jumpctl mcp config --no-tls
```

`config` 无修改选项时只查看配置；带修改选项时，一条命令就是一次保存：仅修改指定字段，全部校验成功后才写入，再完整显示已保存配置。显式 `--allow-lan=false` 可以关闭局域网访问，也属于修改；`--help` 等展示选项不会保存配置。`config` 不设 `set`、`get` 子命令或兼容别名。`--token` 和 `--rotate-token` 互斥；`--no-tls` 与两个 TLS 路径选项互斥。配置 Token 必填，首次保存时可以把网络设置和 `--rotate-token` 放在同一条命令中。所有配置修改均在服务下次启动或重启时生效，运行中的服务继续使用原地址和 Token。

### 查看状态与日志

在另一个终端查看运行中的服务：

```text
jumpctl mcp status
```

状态显示服务宿主、实际地址、启动时间及是否需要重启；不会接管另一个 CLI 或 GUI 进程。已有服务时重复执行 `serve` 会报告现有实例，不会换端口启动第二个服务。更换宿主须先停止原服务，原来的连接和任务 ID 随之失效。

前台日志默认使用“标准”详细程度。可以选择详细日志，或将日志保存为纯文本：

```text
jumpctl mcp serve --log-level detailed
jumpctl mcp serve --color never > mcp.log 2>&1
```

`--log-level` 的 `minimal`、`standard`、`detailed` 分别对应 GUI 的精简、标准、详细。`INFO`／`DEBUG` 写 stdout，`WARN`／`ERROR` 和宿主诊断写 stderr；`2>&1` 将两个流一起收集。非零远端退出码仍是正常命令结果，日志保留真实退出码，不把远端 stdout/stderr 正文直接输出到本地终端。

颜色默认 `--color auto`，终端支持 ANSI 时保留 GUI 对应的颜色，重定向到文件或管道时使用纯文本；`always` 强制保留，`never` 关闭。具体颜色由终端主题决定。完整选项见 [CLI 命令参考](../cli.md#mcp-server)。

## Linux/WSL2 接入 Codex 与 Cursor

以下流程适用于 Ubuntu 24.04 LTS 的 amd64/arm64；当前真实环境验收覆盖 WSL2 amd64，其他验证边界见 [Linux 支持设计](../specs/linux-support.md#73-已完成的验收与待确认项)。示例仅使用虚构站点和 Profile。`jumpctl` 已在 `PATH` 时可直接执行，否则使用自己安装的二进制绝对路径。

### 1. 准备登录与回环服务

先按 [CLI 入门](cli-quick-start.md)创建 Profile 并完成登录。站点应为根地址，例如 `https://jump.example.com`，不要包含 `/core/auth/login/` 或浏览器查询参数。已有 Profile 用 `profile update` 而不是重复 `profile add`；修改站点会清除该 Profile 旧凭据，需要重新登录。

```bash
jumpctl auth login --profile demo --no-browser
jumpctl auth status --profile demo
jumpctl organization list --profile demo
```

授权回调只粘贴到等待中的本机终端，不发到 Agent 对话。首次为 MCP 配置 Token 时，在本机终端执行：

```bash
umask 077
jumpctl mcp config --listen-host 127.0.0.1 --port 39287 \
  --allow-lan=false --no-tls --rotate-token >/dev/null
jumpctl mcp serve --log-level standard
```

`config` 会保存并回显明文 Token，上例丢弃回显；Token 按项目原生设计保存在应用目录的 `config.toml`，Linux 应限制为当前用户可访问的 `0600` 文件。首次配置后，正常停止、重启或电脑重启只需重新运行 `serve`，不要每次轮换 Token。保留服务终端运行；默认地址为 `http://127.0.0.1:39287/mcp`。

### 2. 选择客户端接入方式

JumpAccess 提供 **Streamable HTTP**。客户端可以使用 HTTP 地址及其受保护的认证来源；Codex 的本地 HTTP 连接还支持读取认证头的 helper，具体字段以 [Codex 官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)为准。MCP Token 与 JumpServer OAuth Token 是不同凭据，客户端仅需要前者，不要复制 OAuth 凭据目录。

若要让 Codex 与 Cursor 共用启动命令、运行时读取原生 Token，并在入口处统一限制工具，可以另外部署受控的 stdio → HTTP 适配器。适配器应只连接回环服务，检查凭据文件所有者与权限，禁止在日志中输出 Token，既过滤工具列表也拒绝调用未允许的工具。适配器的 stdout 只能传递 MCP 协议，诊断写 stderr；原 HTTP 服务仍由前面的独立终端承载。

**本仓库不随 `jumpctl` 分发该适配器或认证 helper。** 下面是已另行部署适配器后的配置骨架，必须将占位路径替换为本机实际入口；不是新增的 `jumpctl` 命令。不要把 `jumpctl mcp serve` 直接注册成 stdio MCP，它的 stdout 是服务日志。

在运行 Codex 的 Linux 环境中，将下列条目合并到 `~/.codex/config.toml`，保留已有服务器：

```toml
[mcp_servers.jumpaccess]
command = "/absolute/path/to/jumpaccess-mcp"
enabled_tools = ["get_server_info", "list_profiles", "list_aliases", "list_organizations", "list_assets", "get_asset", "list_accounts"]
startup_timeout_sec = 30
tool_timeout_sec = 60
default_tools_approval_mode = "prompt"
```

在 Cursor 实际加载的用户级 `mcp.json` 中合并条目；Linux 用户级路径为 `~/.cursor/mcp.json`：

```json
{
  "mcpServers": {
    "jumpaccess": {
      "command": "/absolute/path/to/jumpaccess-mcp",
      "args": []
    }
  }
}
```

两客户端引用同一入口，不把 Token 或完整认证头写入客户端配置、命令参数、仓库或 Shell 启动文件。Cursor 示例的只读边界由适配器强制执行；不要假定 Codex 的 `enabled_tools` 会约束另一个客户端。[Cursor 官方文档](https://cursor.com/docs/mcp)维护其当前配置位置、启停入口和日志查看方式。

WSL2 场景优先让客户端的 MCP 进程和 HTTP 服务运行在同一 Linux 环境。Cursor 中打开 WSL 终端并不能证明 MCP 命令也在 WSL 中执行，应核对窗口的远程环境与实际配置来源。Windows 原生进程不能直接执行 Linux 的 `/absolute/path/...`；若 GUI 使用 Windows 配置，需另行安排显式的 WSL 启动入口并重新验收，不通过开放 `0.0.0.0` 规避环境差异。

### 3. 验证原生调用及权限

服务运行后，在客户端重新加载 MCP 配置并建立新连接。可分别运行 `codex mcp get jumpaccess` 检查配置解析、`cursor-agent mcp list-tools jumpaccess` 检查 Cursor CLI 工具发现；部分安装的 Cursor CLI 命令名为 `agent`。这两项不代替实际模型调用。

在 Codex 对话和 Cursor Agent 对话中分别请求：

> 只通过 jumpaccess MCP 的 list_organizations 查询 profile=demo 的组织名称，不使用终端、Python 或历史回答。

确认工具调用卡片或原生调用记录显示 `jumpaccess`，返回 `ok=true` 和该 Profile 实际有权访问的组织。若使用上述只读适配器，两个客户端均应只发现 7 项查询工具；服务自身仍提供完整工具集。需要执行或下载时，由操作者另外授权并调整适配器与客户端权限，保持逐次审批，结束后可恢复只读配置。

### 4. 停止、重启与排障

在服务终端按 Ctrl+C 正常停止；先确认没有其他客户端的活动命令或下载。正常退出后，在同一应用目录下重新运行 `jumpctl mcp serve --log-level standard`。电脑或 WSL 重启后也需手工启动，本流程不安装 systemd 服务或开机自启。

另开终端运行 `jumpctl mcp status` 检查状态。服务重启后，客户端如未自动恢复，执行客户端支持的 MCP 重载/重连；旧连接、执行和下载 ID 会返回 `INSTANCE_CHANGED`，应重新查询状态和打开连接，不能自动重放先前命令。若在本机终端轮换 Token，需重启服务并让客户端重新读取凭据。

| 现象 | 检查与处理 |
| --- | --- |
| `Connection refused` | 先检查服务是否启动、端口是否一致，以及客户端和服务是否处于同一 WSL/Linux 网络环境 |
| `401` | 核对接入入口读取的 MCP Token 是否与运行中服务一致；不要上传配置文件或打印认证头 |
| `ENOENT`、入口不存在 | 核对占位路径已替换、入口可执行、依赖齐全，以及 GUI 是否误在 Windows 中执行 Linux 路径 |
| 显示工具数量但状态 `unknown`，或原生调用报告未知服务器 | 磁盘配置与当前会话运行态可能不同；重载 MCP 配置并重新连接。仅退出终端 UI 可能仍复用原后台服务，不以临时工具发现代替本会话调用成功 |
| 能调用但缺少执行/下载工具 | 检查适配器和客户端白名单；7 项只读配置下属于预期行为，不要全量自动批准 |
| 重复启动报已有实例 | 在现有宿主中管理服务，不另换端口或强制结束其他客户端任务 |
| `decode OAuth discovery response` 且出现 `<` | 检查 Profile 是否误填登录页面地址；根地址正确后再检查代理或认证网关是否返回 HTML |

Cursor 可在 Customize/MCP 中重连，并在 Output 面板的 MCP 日志中查看初始化和调用错误。共享日志前先移除凭据、回调 URL 和业务数据。回滚接入配置前备份现有文件，保留其他服务器条目；移除客户端条目不需要删除 JumpAccess 的 OAuth 凭据或代理私钥。

## 让 Agent 下载文件

可以直接说：“把 work 环境 prod-db 上最新的应用日志下载到我的下载目录，完成后告诉我保存位置。”支持单个文件、多个文件、目录和正在写入的日志，也可以指定保存位置。

仅供本机访问时，文件直接保存到运行 JumpAccess 的电脑；开启局域网共享监听，或服务没有目标目录的写入权限时，提供下载链接供调用方保存。链接就绪不代表已经保存到你的电脑。直接保存的结果可直接使用，无需再解压或校验；单个文件或目录保留原名和层级，多个独立对象放在一个结果目录，重名另存。

软件自动选择 SFTP 或 `sz`，在服务器上准备压缩文件，不修改源文件，也不占用正在使用的终端。单文件链接通常为 `.gz`，目录或多文件为 `.tar.gz`；正常下载、按说明解压即可，无需额外计算哈希或回服务器核验。自动添加的压缩包在成功解压后清理；原本就是压缩文件的目标保持原样。日志读取开始后的新增内容留给下次下载。

下载期间，服务器的 home 或其他可用暂存位置可能出现 `.jumpaccess-download-<时间>-<UUID>.gz`、`.tar.gz`、`.part` 或 `.jumpaccess-probe-` 开头的临时文件，正常结束后自动清理。异常退出可能遗留远端文件，重启应用不会自动清扫它们。

下载链接就绪后有效 30 分钟；停止或重启 MCP 会使旧链接失效。领取文件暂存在 JumpAccess 自管缓存目录，到期后自动清理，不影响已经保存的最终文件。GUI 可在 MCP“日志”页查看进度和异常，CLI 通过 `serve` 的持续日志查看；关闭 GUI 的 MCP 页签不停止下载，退出宿主或停止服务会停止任务。

每项任务传输的压缩包或原样文件最多 1 GiB；完整环境要求和边界见 [MCP 文件传输设计](../specs/agent-mcp-file-transfer.md)。
