# jumpctl CLI 入门：从安装到第一次 SSH 连接

`jumpctl` 是当前项目提供的用于连接 [JumpServer](https://github.com/jumpserver/jumpserver) 的 CLI 程序。

本文带你完成一次完整的连接。你将先登录 JumpServer，找到有权访问的资产并建立 SSH 会话，再绑定别名并用别名连接。最后可以选择给 `jumpctl ssh` 设置快捷命令，让一次 SSH 连接简化为：

```sh
jssh web
```

也可以配置 OpenSSH，使用：

```sh
ssh web
```

先按顺序完成第 1–9 步，在已有可用 JumpServer 的前提下，这通常只需要 5 分钟。再从第 10.1、10.2 节中选择一种日常连接方式。除特别标明“在远端执行”的命令外，所有命令都在你自己的电脑上执行。

如果还不熟悉 Profile、组织（Organization）、资产（Asset）和账号（Account），可以先花一分钟阅读[数据层级说明](data-hierarchy.md)。

## 1. 准备好 JumpServer 地址和账号

开始前，你需要：

- 一个可以在浏览器中访问的 JumpServer 站点地址，例如 `https://jump.example.com`。
- 可以登录该站点的用户账号，以及站点要求的 MFA 等验证方式。
- 至少一台已向你授权 SSH 访问的资产，以及该资产上的可用账号。尚未获得权限时，请联系 JumpServer 管理员。

这里有两种“账号”：

- **JumpServer 用户账号** 是浏览器登录使用的。
- **资产账号** 是登录远程主机的账号，例如 `deploy`。

本文使用示例值，如 URL 、组织 ID 、资产 ID 和账号，需换成你自己的值。

## 2. 安装 jumpctl

如果你有 Go 1.25 或更高版本环境，直接执行：

```powershell
go install github.com/cmstar/jumpaccess/cmd/jumpctl@latest
```

如果没有 Go 环境，可打开 [JumpAccess 发布页](https://github.com/cmstar/jumpaccess/releases)，下载并解压出已经编译好的可执行文件。

| 你的系统 | 对应文件 |
| --- | --- |
| Windows x64 | `jumpctl-v版本号-windows-amd64.zip` |
| macOS，Apple 芯片，ARM64 架构 | `jumpctl-v版本号-darwin-arm64.tar.gz` |
| macOS，Intel 芯片，x86-64 架构 | `jumpctl-v版本号-darwin-amd64.tar.gz` |

下载后，如需要任意位置可用，需自行添加到 `PATH` 环境变量。若通过 Go 命令安装，则通常已在 `PATH` 里。

> 直接下载的可执行文件没有进行 Windows/macOS 签名，新下载的文件首次运行时操作系统可能显示安全提醒，放行后可正常使用，后续不会再次提示。

验证安装：

```powershell
jumpctl version
```

## 3. 添加并启用 JumpServer 连接配置

`Profile` 是本机保存的一份 JumpServer 连接配置。

下面的命令创建名为 `work` 的 Profile ，把 URL 换成你平时访问的 JumpServer 站点地址。

```sh
jumpctl profile add work --url https://jump.example.com
```

列出当前的 Profile ：

```sh
jumpctl profile list
```

示例输出：

```text
CURRENT  PROFILE  URL                       ORGANIZATION
*        work     https://jump.example.com  未设置
```

`ORGANIZATION` 显示该 Profile 配置的组织名称，未配置时显示“未设置”。GUI 资产页切换组织后，会保存到对应 Profile，并反映在此列。未登录或组织名称暂时无法查询时，显示组织 ID 和“（名称暂不可用）”。

`*` 表示当前启用的 Profile。添加的第一个 Profile 会自动启用。如果需要切换到指定的 Profile ，执行：

```sh
jumpctl profile use work
```

## 4. 开始浏览器认证，并把地址粘贴回来

新建的 Profile 需要完成 JumpServer 的授权才可以访问远程资源。

### 4.1 发起登录

在本机终端执行：

```sh
jumpctl auth login
```

程序会打开系统浏览器，终端停在下面的提示处：

```text
OAuth callback URL:
```

如果本机没有可用浏览器，或希望在另一台电脑上完成授权，使用：

```sh
jumpctl auth login --no-browser
```

程序会打印授权地址并等待回调，不打开本机浏览器。复制该地址到能访问 JumpServer 的浏览器中，继续下面的步骤。无需同时指定 `--manual`；保持原终端进程运行，在默认 5 分钟的登录超时内把回调粘贴回来，Token 会保存在运行 CLI 的机器上。当前平台支持仍为 Windows 和 macOS，Linux 适配尚未完成。

### 4.2 在浏览器中完成登录和授权

1. 在上一步打开的 JumpServer 页面上登录你的用户账号。如果要求 MFA 或其他验证，按页面提示完成。
2. 出现授权页面，完成授权，直到进入提示将要跳转到 `jms://` 地址的外部跳转确认页。

**到达这个外部跳转确认页后，不要点击该页的“确认”按钮。** 这里需要复制地址，不需要启动系统中的 `jms://` 协议处理程序。

### 4.3 复制完整地址

推荐直接复制浏览器地址栏中的完整确认页 URL 。该地址通常含有 `next=jms%3A...`。也可以复制页面中完整的 `jms://auth/callback?...` 链接，二选一即可。无需自行解码或截取参数。

### 4.4 回到原终端，粘贴并按 Enter

切回仍在等待 `OAuth callback URL:` 的终端，把刚才复制的完整地址直接粘贴进去（无需添加引号），然后按 Enter 。

后续授权流程会自动进行，成功后会显示：

```text
authenticated profile work
```

此时可以关闭浏览器确认页。如果命令已超时退出，或提示 `state did not match`，重新执行 4.1 的登录命令。

### 4.5 检查认证状态

```sh
jumpctl auth status
```

检查输出中的 `profile: work` 和 `status: authenticated`。另外两项会显示 Access Token 过期时间，以及是否可通过 Refresh Token 续期。

认证完成后，就可以查询自己有权访问的资源了。

## 5. 先列出组织

组织（ Organization ）用于划分资源。先执行：

```sh
jumpctl organization list
```

示例输出（下面所有 `*-demo` ID 都是虚构值）：

```text
NAME    ID
运维组  org-demo
```

根据 `NAME` 找到目标资产所属的组织，复制同一行的 **`ID`**。后面的 `--organization` 接收组织 ID。

下文以 `org-demo` 为例，请在每条命令中换成刚查到的真实组织 ID。执行 `organization list` 只列出组织，并不会自动切换组织；资源查询和连接命令中的 `--organization` 也只对当次命令生效。因此，第 6–8 步都需要显式指定组织，第 9 步再将组织保存到别名中。

## 6. 列出资产

> 如果你的 JumpServer 的默认组织就是你需要的组织，则可以省略下文的 `--organization` 参数。

```sh
jumpctl asset list --organization org-demo
```

示例输出：

```text
NAME        ADDRESS     TYPE   ID
web-server  192.0.2.10  linux  asset-demo
```

## 7. 列出该资产允许使用的账号

使用资产 ID / 名称 / 地址列出资产下的账号：

```sh
jumpctl account list web-server --organization org-demo
```

示例输出：

```text
NAME      USERNAME  ID
部署账号  deploy    account-demo
```

> 资产名称和地址都可能重复。当前 CLI 按服务端返回顺序选择第一个名称或地址完整匹配的资产，匹配时不区分大小写，不会因资产重名而报错。需要明确指定目标时，请使用该资产的 ID ，并指定正确的组织。

## 8. 建立 SSH 连接

使用资产 ID / 名称 / 地址发起连接：

```sh
jumpctl ssh web-server --organization org-demo --account deploy
```

> `--account` 可以不添加，如果资产只有一个可用账号，会自动使用；如果有多个， CLI 会询问需要使用哪个。

首次遇到未知的 JumpServer SSH 网关时，可能显示类似提示：

```text
Unknown SSH host key for gateway.example.com:2222
Fingerprint: SHA256:...
Trust this host key? [y/N]:
```

这里显示的是 **JumpServer SSH 网关的指纹**。向管理员核对网关地址和指纹，确认一致后输入 `y` 并按 Enter。确认记录会保存在本机，后续连接同一网关通常不会再次询问。

连接成功后，终端中的命令会在远端执行。完成体验后，在远端执行 `exit` 退出 SSH 会话，回到本机终端后再继续第 9 步。

## 9. 给资产和账号绑定一个别名

使用别名，可以让资产名称更容易记忆，同时可以直接绑定需要使用的账号，在资产有多个可用账号时，跳过账号选择步骤。

添加（若存在则更新）别名，并绑定组织、资产和账号：

```sh
jumpctl alias set web --organization org-demo --asset asset-demo --account account-demo
```

检查保存结果：

```sh
jumpctl alias list
```

示例输出：

```text
ALIAS  ASSET       ADDRESS     ACCOUNT             ORGANIZATION
web    web-server  192.0.2.10  部署账号（deploy）  运维组
```

查询成功时，列表显示资产名称、地址、账号名称及用户名、组织名称。若要核对保存的原始 ID 或引用，可运行 `jumpctl alias list --long` 。

接下来可以通过别名进行 SSH 连接：

```sh
jumpctl ssh web
```

## 10. 快捷调用

### 10.1 方案一：设置 jssh 快捷命令

本节让你能够通过短命令 `jssh web` 直接连接资产/别名 `web` 。

可为 `jumpctl ssh` 设置快捷命令，让一次 SSH 连接 `jumpctl ssh web` 简化为 `jssh web` 。

根据你的 shell 类型，在 macOS 的 zsh `~/.zshrc` 或 Bash `~/.bashrc` 添加别名：

```sh
alias jssh='jumpctl ssh'
```

Windows 可创建批处理文件 `jssh.bat` ，并将其所在目录加入 `PATH` ：

```bat
@echo off
jumpctl ssh %1 %2 %3 %4 %5 %6 %7 %8 %9
```

### 10.2 方案二：配置 OpenSSH，以后直接使用 ssh 命令

本节让你能够通过 OpenSSH 命令 `ssh web` 直接连接资产/别名 `web` 。

`jumpctl ssh` 使用 JumpAccess 自带的 SSH 客户端。现在让本机 OpenSSH 的 `ssh` 调用 `jumpctl proxy` 完成连接准备，你就可以继续使用熟悉的 `ssh` 命令。

编辑 OpenSSH 配置文件，配置文件位于当前用户主目录的 `~/.ssh` 文件夹中，文件名是 **`config`，没有扩展名**，如果没有，可以直接新建一个。

将下面内容添加到文件中，保留已有配置。如果文件中有 `Host *` 通用配置，把这一块放在它前面：

```sshconfig
Host web
    HostName web
    User deploy
    ProxyCommand jumpctl proxy "%h" --profile work --account "%r"
```

这几项与前面的操作对应：

| 配置项 | 本例含义 |
| --- | --- |
| `Host web` | 你在本机输入 `ssh web` 时使用的名称 |
| `HostName web` | 对应 JumpAccess 里的资产名称/ID/别名 |
| `User deploy` | 对应 jumpctl 的 `--account` 参数 |
| `ProxyCommand jumpctl proxy "%h" --profile work --account "%r"` | 让 OpenSSH 启动代理；`%h` 会替换成 `HostName` 的值 `web`，`%r` 会替换成 SSH 传给 jumpctl 的用户名，`--profile work` 固定使用该 Profile |

ProxyCommand 这里直接指定了 `--profile` ，这样不会受到用户切换当前 Profile 的影响，是推荐做法。

如果连接的别名已绑定了账号，或者资产下只有一个账号，可忽略 `--account` 参数，配置简化为：

```sshconfig
Host web
    HostName web
    ProxyCommand jumpctl proxy "%h" --profile work
```

同理，也可以省略 `HostName` ，将目标直接写到 `ProxyCommand` 中，这种写法最为简单：

```sshconfig
Host web
    ProxyCommand jumpctl proxy web --profile work
```

> 关于配置匹配顺序和 `%h`/`%r`，可查阅 [OpenSSH 配置手册](https://man.openbsd.org/ssh_config)。

如果 `jumpctl` 不在 `PATH` 里，可使用完整路径，例如 macOS 上：

```sshconfig
    ProxyCommand "/Users/yourname/bin/jumpctl" proxy "%h" --profile work
```

完成配置后，就可以使用 `ssh {配置里的 Host 名称}` 快捷发起连接了。

首次通过 OpenSSH 连接时，可能再次出现主机密钥确认提示。这次看到的是本机 JumpAccess 代理提供的稳定主机密钥，与第 8 步确认的 JumpServer SSH 网关密钥不同。 OpenSSH 的信任记录通常保存在 `~/.ssh/known_hosts` ， JumpAccess 则在自己的应用目录中保存网关信任记录；两者独立验证，不应直接比较这两个指纹。

## 11. 使用命令前缀

`jumpctl` 支持用命令的前缀代替完整名称，只要前缀能在当前层级唯一确定一个命令即可。各级子命令都支持这种写法，例如下面几条命令都等同于 `jumpctl organization list` ：

```sh
jumpctl org list
jumpctl org l
jumpctl o l
```

这里的 `org` 也是 `organization` 已注册的命令别名。完整命令名和已注册别名优先匹配；未精确匹配时，才按唯一前缀查找命令。

连接命令也可以缩写，例如 `jumpctl s web` 等同于 `jumpctl ssh web` 。

如果前缀同时匹配多个命令，程序会报错并列出候选，不会执行其中任何一个。此时多输入几个字母，或使用完整名称即可：

| 有歧义的写法 | 原因 | 可用的写法 |
| --- | --- | --- |
| `jumpctl pr` | 同时匹配 `profile` 和 `proxy` | `jumpctl prof list` 或 `jumpctl prox web` |
| `jumpctl auth l` | 同时匹配 `login` 和 `logout` | `jumpctl auth logi` 或 `jumpctl auth logo` |

前缀匹配只作用于子命令，不适用于选项名：例如 `--profile` 不能缩写为 `--prof` ， `--organization` 也不能缩写为 `--org` 。

日常手动输入时可以使用前缀；脚本和 OpenSSH 的 `ProxyCommand` 配置建议保留完整命令名，避免未来新增命令后产生歧义。

## 12. 后续使用与常见问题

Profile 和别名会保存在本机，不必每次重新创建。程序会在请求前按需续期；如果提示需要重新登录，在本机执行：

```sh
jumpctl auth login --profile work --manual
```

按第 4 步重新完成浏览器认证，再按所选方案运行 `jssh web --profile work` 或 `ssh web`。OpenSSH 的代理模式不会自行打开浏览器登录。

| 遇到的情况 | 下一步 |
| --- | --- |
| 粘贴地址后提示回调不匹配或超时 | 重新发起登录，复制这一次最终确认页的完整地址，粘贴回同一个等待中的终端 |
| 组织、资产或账号只有列头 | 检查 Profile、组织 ID 和搜索条件，再确认 JumpServer 授权 |
| 资产名称或地址重复，或提示账号不唯一 | 从列表复制目标资产和账号的 ID ，重新执行第 9 步，绑定正确的组织、资产和账号 |
| `jumpctl ssh` 成功，但 `ssh web` 找不到程序 | 检查 OpenSSH 配置里的 `ProxyCommand`，改用 `jumpctl` 的完整路径 |
| 代理提示未知上游主机密钥 | 先运行 `jumpctl ssh web --profile work`，核对并确认它实际连接的网关，再重试 |
| 无法连接网关或连接超时 | 检查 VPN、网络和网关可达性，并联系管理员确认 SSH 访问策略 |

需要查看某条命令的全部参数时，使用 `--help`，例如：

```sh
jumpctl asset list --help
jumpctl ssh --help
jumpctl proxy --help
```

连接后需要传输文件时，继续阅读[SSH 终端内上传和下载文件](file-transfer.md)，了解 `rz` / `sz` 和 CLI 下载目录设置。

[返回用户指南](README.md) · [CLI 命令参考](../cli.md)
