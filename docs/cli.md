# `jumpctl` CLI 命令参考

本文只列出当前代码中已经存在的命令。所有命令都可以用 `--help` 查看即时参数说明；Shell completion 由 Cobra 的内置 `completion` 命令生成。

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

浏览器登录由 `auth login` 单独完成。真实账号、MFA 和密码不应作为命令参数传递；授权在系统浏览器中进行，OAuth Token 保存到 Windows Credential Manager 或 macOS Keychain。当前开发版本默认要求粘贴手工回调；正式发布版计划默认注册并接收 `jms` 私有协议，同时永久保留 `--manual` 作为显式回退方式。

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
| `jumpctl profile list` | 列出 Profile，并用 `*` 标记当前项 |
| `jumpctl profile use <name>` | 切换当前 Profile |
| `jumpctl alias set <name> --asset <asset> [--account <account>] [--organization <org>] [--profile <name>]` | 创建或替换 Profile 范围内的 Alias |
| `jumpctl alias list [--profile <name>]` | 列出 Alias |

Alias 适合批量直接编辑。非敏感配置的根目录为：

- Windows：`%LOCALAPPDATA%\JumpAccess`
- macOS：`~/Library/Application Support/JumpAccess`

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
| `jumpctl auth login [--profile <name>] [--manual]` | 打开浏览器并完成 Authorization Code + PKCE 登录；`--manual` 强制从终端读取粘贴的回调 URL，当前开发版本未实现私有协议接收时也默认使用该方式 |
| `jumpctl auth status [--profile <name>]` | 只显示登录状态、过期时间和是否有 Refresh Token，不显示秘密 |
| `jumpctl auth refresh [--profile <name>]` | 立即刷新；Refresh Token 轮换后原子写回原生凭据存储 |
| `jumpctl auth logout [--profile <name>]` | 撤销并删除 Profile 的 OAuth 凭据 |

程序发起 API 请求前会按需刷新。直接 SSH 和 ProxyCommand 运行期间还会定期检查；刷新成功只影响后续 API 请求，刷新失败会写入 stderr，但不会关闭已经建立的 SSH Session。

手工回调时，JumpServer 授权完成后会显示外部跳转确认页。不要点击“确认”，复制以下任一种内容并直接粘贴到正在等待的终端：

- 页面中显示的完整 `jms://auth/callback?...` 链接。
- 浏览器地址栏中带有 `next=jms%3A...` 的完整 JumpServer 确认页 URL。

程序只接受目标严格匹配 `jms://auth/callback` 且 `state` 与当前登录流程一致的回调。Authorization Code 只用于当前 Token 交换，不会写入配置或普通输出。

## 资源发现

| 命令 | 作用 |
| --- | --- |
| `jumpctl organization list [--profile <name>]` | 合并并列出当前用户有权使用的 Organization；`org` 是同义命令 |
| `jumpctl asset list [--profile <name>] [--organization <org>] [--search <text>]` | 列出最多 100 个匹配 Asset |
| `jumpctl account list <asset> [--profile <name>] [--organization <org>]` | 精确解析 Asset，并列出其允许的 Account |

Asset 引用可以是 ID、名称或地址，但必须精确匹配且唯一。自动化和 ProxyCommand 建议在 Alias 中保存稳定 ID。

所有 `list` 命令都输出带大写列头的文本表格，并根据本次结果中的最长内容自动对齐；没有结果时仍输出列头。当前各命令的列为：

| 命令 | 列头 |
| --- | --- |
| `profile list` | `CURRENT`、`PROFILE`、`URL` |
| `alias list` | `ALIAS`、`ASSET`、`ACCOUNT`、`ORGANIZATION` |
| `organization list` | `ID`、`NAME` |
| `asset list` | `ID`、`NAME`、`ADDRESS`、`TYPE` |
| `account list` | `ID`、`USERNAME`、`NAME` |

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

Proxy 模式是非交互的 SSH server façade：stdout 只承载 SSH 协议字节，诊断只写 stderr。它不会打开浏览器、选择 Account 或信任未知上游 host key。目标和 Account 必须由 Alias 或显式参数唯一确定。

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
