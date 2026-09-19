# `jumpctl` CLI 命令参考

本文只列出当前代码中已经存在的命令。所有命令都可以用 `--help` 查看即时参数说明；Shell completion 由 Cobra 的内置 `completion` 命令生成。

## 命令缩写

各层级命令支持区分大小写的唯一前缀匹配。完整命令名和已注册别名优先；未精确命中时，只有当前层级唯一匹配的前缀才会执行。例如以下命令均等同于 `jumpctl organization list`：

```text
jumpctl org l
jumpctl or l
jumpctl orga li
jumpctl o l
```

前缀同时匹配一个命令的名称和多个别名时，只算一个候选。存在歧义时不执行，向 stderr 列出匹配的正式命令名，并返回非零退出码：

```text
jumpctl auth l
ambiguous command "l" for "jumpctl auth"
Available commands:
  login
  logout
```

`pr` 会匹配 `profile`、`proxy`；`profile u` 会匹配 `update`、`use`；`c` 会匹配 `completion`、`config`。没有匹配时报告 `unknown command`。只输入命令组（如 `jumpctl or`）仍显示帮助；`jumpctl help or l` 支持相同的匹配和歧义检查。

缩写仅适用于命令名及命令别名，不适用于 `--profile` 等选项名，也不会改写 Profile、Asset、Account、Alias 或 SSH 目标等参数值。`--` 后的内容仍作为位置参数处理。脚本和 SSH `ProxyCommand` 配置建议保留完整命令名，避免未来新增命令后缩写产生歧义。

## 建议的首次使用流程

```text
jumpctl profile add work --url https://jump.example.com
jumpctl auth login
jumpctl organization list
jumpctl asset list --organization <org-id> --search <asset-name>
jumpctl account list <asset-id> --organization <org-id>
jumpctl alias set web --asset <asset-id> --account <account-id> --organization <org-id>
jumpctl ssh web
```

浏览器登录由 `auth login` 单独完成。真实账号、MFA 和密码不应作为命令参数传递；授权在系统浏览器中进行，OAuth Token 按 Profile 保存到应用根目录内受严格权限保护的 `credentials` 子目录。当前开发版本默认要求粘贴手工回调；正式发布版计划默认注册并接收 `jms` 私有协议，同时永久保留 `--manual` 作为显式回退方式。

## 通用命令

| 命令 | 作用 |
| --- | --- |
| `jumpctl version` | 输出程序版本 |
| `jumpctl licenses` | 输出内嵌的 JumpAccess MIT 许可证和第三方软件许可材料；`license` 是同义命令 |
| `jumpctl help [command]` | 查看帮助 |
| `jumpctl completion <shell>` | 生成 bash、zsh、fish 或 PowerShell completion |

## 配置、Profile 与 Alias

| 命令 | 作用 |
| --- | --- |
| `jumpctl config path` | 输出 `config.toml` 的绝对路径 |
| `jumpctl config edit` | 不存在时创建默认配置，并用系统默认编辑器打开 |
| `jumpctl config validate` | 严格解析 TOML，拒绝未知字段和非法值 |
| `jumpctl profile add <name> --url <site>` | 新增 JumpServer Profile；第一个 Profile 自动成为当前项 |
| `jumpctl profile update <name> --url <site>` | 修改 Profile 的 Server URL；保留 Organization 和 Alias，URL 变化时清除旧 OAuth 凭据并要求重新登录 |
| `jumpctl profile delete <name>` | 删除 Profile、Organization、Alias 和本地 OAuth 凭据；只有交互输入精确的 `yes` 后才执行，删除当前项后自动选择下一个 Profile |
| `jumpctl profile list` | 列出 Profile，并用 `*` 标记当前项 |
| `jumpctl profile use <name>` | 切换当前 Profile |
| `jumpctl alias set <name> --asset <asset> [--account <account>] [--organization <org>] [--profile <name>]` | 创建或替换 Profile 范围内的 Alias |
| `jumpctl alias list [--profile <name>] [--long]` | 列出 Alias 对应的资产名称、地址、账号和组织；`--long` 逐项显示完整引用 |

Alias 适合批量直接编辑。每用户应用根目录为：

- Windows：`%LOCALAPPDATA%\JumpAccess`
- macOS：`~/Library/Application Support/JumpAccess`

Profile 名保持用户输入的精确值，不按文件名规则改写。名称不能是空值、不能带首尾空白或控制字符，也不能是 `.`、`..`；Unicode、路径分隔符和平台保留名由凭据文件名摘要隔离，不会直接进入路径。

示例 TOML：

```toml
version = 1
current_profile = "work"

[behavior]
refresh_check_interval = "30s"
refresh_before_expiry = "1m"
connect_timeout = "30s"
oauth_timeout = "5m"

[profiles.work]
url = "https://jump.example.com"
organization = "org-id"

[profiles.work.aliases.web]
asset = "asset-id"
account = "account-id"
```

## OAuth

| 命令 | 作用 |
| --- | --- |
| `jumpctl auth login [--profile <name>] [--manual] [--no-browser]` | 完成 Authorization Code + PKCE 登录；默认打开浏览器，`--manual` 强制手工粘贴回调（当前版本也是默认方式），`--no-browser` 跳过打开浏览器并等待粘贴回调 |
| `jumpctl auth status [--profile <name>]` | 只显示登录状态、过期时间和是否有 Refresh Token，不显示秘密 |
| `jumpctl auth refresh [--profile <name>]` | 立即刷新；Refresh Token 轮换后原子写回该 Profile 的凭据文件 |
| `jumpctl auth logout [--profile <name>]` | 撤销并删除 Profile 的 OAuth 凭据 |

本机无法打开浏览器，或希望在另一台电脑完成授权时，运行：

```text
jumpctl auth login --profile work --no-browser
```

程序向 stderr 打印完整授权地址，等待手工粘贴回调，不调用本机浏览器。将授权地址复制到可以访问 JumpServer 的浏览器中，完成授权后按下文复制回调。`--no-browser` 无需搭配 `--manual`，也允许两者同时使用；单独使用 `--manual` 仍会打开浏览器。

保持原登录进程运行，并在 `behavior.oauth_timeout`（默认 5 分钟）内粘贴回调；超时后重新发起登录并使用新的授权地址。Token 由运行 CLI 的机器交换和保存。本参数不改变平台支持范围，当前支持 Windows 和 macOS，Linux 适配尚未完成。

程序发起 API 请求前会按需刷新。直接 SSH 和 ProxyCommand 运行期间还会定期检查；刷新成功只影响后续 API 请求，刷新失败会写入 stderr，但不会关闭已经建立的 SSH Session。

凭据文件是包含 Access Token 和 Refresh Token 的明文 JSON，必须像 SSH 私钥一样保护。Windows 仅允许当前用户与 `SYSTEM` 访问，macOS 要求当前用户所有的 `0700` 目录与 `0600` 文件；权限、所有者或路径类型不符合要求时，程序拒绝读取。OAuth Token 只使用这些文件，不读取 Windows Credential Manager 或 macOS Keychain。

手工回调时，JumpServer 授权完成后会显示外部跳转确认页。不要点击“确认”，复制以下任一种内容并直接粘贴到正在等待的终端：

- 页面中显示的完整 `jms://auth/callback?...` 链接。
- 浏览器地址栏中带有 `next=jms%3A...` 的完整 JumpServer 确认页 URL。

程序只接受目标严格匹配 `jms://auth/callback` 且 `state` 与当前登录流程一致的回调。Authorization Code 只用于当前 Token 交换，不会写入配置或普通输出。

## 资源发现

| 命令 | 作用 |
| --- | --- |
| `jumpctl organization list [--profile <name>]` | 合并并列出当前用户有权使用的 Organization；`org` 是同义命令 |
| `jumpctl asset list [--profile <name>] [--organization <org>] [--search <text>] [--offset <count>] [--limit <count>]` | 分页列出匹配的 Asset；`--offset` 默认为 `0`，`--limit` 默认为 `100` |
| `jumpctl account list <asset> [--profile <name>] [--organization <org>]` | 精确解析 Asset，并列出其允许的 Account |

Asset 引用可以是 ID、名称或地址。UUID 格式的 ID 直接查询详情；其他引用按每页 100 条搜索，名称和地址不区分大小写、完整匹配。按服务端返回顺序使用第一个精确匹配的 Asset，找到后立即停止，不检查或报告重名歧义；当前页没有匹配且还有后续结果时才继续翻页，全部查完仍无匹配时报找不到资产。分页请求失败或无法推进时返回错误。该规则适用于 `account list`、`ssh` 和 `proxy` 等共用资产解析的操作；`asset list` 保留显式分页行为。重名时的选择取决于服务端返回顺序，自动化和 ProxyCommand 建议在 Alias 中保存稳定 ID。

`asset list` 使用偏移量分页。例如，`jumpctl asset list --offset 100 --limit 100` 会跳过前 100 个匹配结果并获取接下来的最多 100 个。`--offset` 必须大于或等于 `0`，`--limit` 必须大于 `0`；不指定分页参数时保留原有的前 100 条行为。

所有 `list` 命令默认输出带大写列头的文本表格，并根据本次结果中的最长显示宽度自动对齐，支持中文和组合字符；没有结果时仍输出列头。当前各命令的列为：

| 命令 | 列头 |
| --- | --- |
| `profile list` | `CURRENT`、`PROFILE`、`URL` |
| `alias list` | `ALIAS`、`ASSET`、`ADDRESS`、`ACCOUNT`、`ORGANIZATION` |
| `organization list` | `NAME`、`ID` |
| `asset list` | `NAME`、`ADDRESS`、`TYPE`、`ID` |
| `account list` | `NAME`、`USERNAME`、`ID` |

资源列表把名称放在前面，完整 ID 放在最后，不截短。Organization 按名称排序，同名时按 ID 排序；Asset 保持当前页内按名称排序，Account 保持按用户名（缺少时按名称或 ID）排序。名称为空或只有空白时显示“名称未提供”。

`alias list` 按别名排序，查询所选 Profile 下对应组织中的资产详情和组织名称：

- `ASSET` 显示资产名称，`ADDRESS` 显示地址；地址不可用时显示 `—`。
- `ACCOUNT` 显示“账号名称（用户名）”；两者相同时只显示一次，名称缺失时依次使用用户名、账号别名。账号引用按 ID、名称、用户名或账号别名精确匹配，后三者不区分大小写；匹配不唯一时不猜测。`@INPUT`、`@USER`、`@ANON` 按原有特殊账号语义显示。
- 未绑定账号显示“未绑定”，不会因资产只有一个账号而把它显示为已绑定。
- 别名未指定组织时继承 Profile 的组织，并在名称后标记“（继承）”；两处均未指定时显示“未设置”。
- 未登录、网络失败、资源不可见或账号引用无法唯一解析时，保留对应原始引用并标记“（名称暂不可用）”，仍成功列出本地 Alias。已查到资源但没有名称时，资产保留原始引用并标记“（名称未提供）”；账号没有名称、用户名或账号别名时同样处理。组织查询失败不阻止资产和账号名称展示。
- 查询复用已有认证和按需 Token 刷新，不打开浏览器或提示选择账号；不修改 Alias 配置。组织列表每次执行最多查询一次，同一组织中的相同资产引用在本次执行内复用查询结果（包括失败结果），不同组织分别查询。用户取消时停止查询并返回非零状态。

示例（数据为虚构）：

```text
ALIAS     ASSET       ADDRESS     ACCOUNT                 ORGANIZATION
order     订单服务    10.20.1.11  应用部署账号（deploy）  生产环境
order-db  订单数据库  10.20.1.12  未绑定                  生产环境（继承）
```

`alias list --long` 为每个 Alias 输出一个独立条目，条目之间空一行：

```text
ALIAS         order
ASSET         订单服务
ASSET REF     738b492a-6e31-482d-9d28-5c0184d2b60f
ADDRESS       10.20.1.11
ACCOUNT       应用部署账号（deploy）
ACCOUNT REF   deploy
ORGANIZATION  生产环境（继承）
ORG REF       b8c95402-756a-498c-8b41-1c73fae192d6
```

`ASSET REF`、`ACCOUNT REF` 保留配置中的完整原始引用，可能是 ID，也可能是名称、地址或用户名，不自动改写为 ID。`ORG REF` 显示实际使用的完整组织引用（包含从 Profile 继承的值）。“继承”标记保留在 `ORGANIZATION` 中；空账号或组织引用分别显示“未绑定”“未设置”。没有 Alias 时，`--long` 输出“暂无 Alias”。

这些表格面向终端阅读；列之间由可变数量的空格分隔，不应把固定空格位置当作稳定的机器解析格式。

## 直接 SSH

```text
jumpctl ssh <target> [--profile <name>] [--organization <org>] [--account <account>]
```

`target` 先按当前 Profile 的 Alias 解析，否则作为 Asset ID、名称或地址查询。没有显式 Account 时，唯一 Account 会自动选择；存在多个 Account 时，直接模式会在终端列出并要求选择。

首次连接未知 JumpServer SSH gateway 时会显示 SHA-256 host key 指纹并要求明确确认。记录写入应用根目录的 `known_hosts`；已知密钥变化始终拒绝。

当前不采集 `@INPUT` 或 `@USER` 所需的密码，遇到这类 Account 会要求改用 JumpServer 托管 Account。`@ANON` 可以无秘密使用。

## 通用 ProxyCommand

```text
jumpctl proxy <target> [--profile <name>] [--organization <org>] [--account <account>]
```

Proxy 模式是非交互的 SSH server façade：stdout 只承载 SSH 协议字节，诊断只写 stderr。它不会打开浏览器、选择 Account 或信任未知上游 host key。Asset 沿用首个精确匹配规则；Account 必须由 Alias、显式参数或唯一可用账号确定，否则报错。

Windows 上，`jumpctl proxy` 会在连接准备和本地 SSH façade 握手期间保留 stderr；握手成功后，如果兼容客户端已通过管道提供 stdin、stdout，且当前控制台只属于 `jumpctl`，进程会脱离该私有控制台。PowerShell、CMD、Windows Terminal、OpenSSH 等共享或交互控制台保持附着；检查无法确认时也保持原状。macOS 不执行这项 Windows 专用处理。远端程序的 stdout 和 stderr 仍作为 SSH channel 数据正常显示在客户端中，不能与 ProxyCommand stdout 上的原始 SSH 传输流混用。

通用 OpenSSH 示例：

```sshconfig
Host production-web
    HostName web
    User jumpaccess
    ProxyCommand jumpctl proxy %h
```

外部 SSH 客户端连接的是 JumpAccess façade，因此会看到一把由 JumpAccess 保存在操作系统凭据存储中的稳定 Ed25519 host key。JumpAccess 再通过自己的 `known_hosts` 验证上游 JumpServer gateway。这两层信任彼此独立。

MVP 只转发 SSH session 能力，包括 env、PTY、shell/exec、窗口变化、信号、stdout、extended stderr 和退出状态；明确拒绝端口转发、agent、X11、SFTP/subsystem 及未知请求。

## 错误与输出约定

- 成功返回状态码 `0`，失败返回非零状态。
- 普通命令的结果写 stdout，错误写 stderr。
- `proxy` 在完成认证、目标解析、Connection Token、上游 host key 校验和上游 SSH 握手之前，不向 stdout 发出 SSH banner。
- 未登录或 Refresh Token 已失效时，错误会提示运行 `jumpctl auth login`。
- Token、密码、Connection Token、client-url 和私钥不得出现在 TOML、普通输出或错误文本中。
