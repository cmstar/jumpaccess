# JumpAccess

[![License: MIT](https://img.shields.io/badge/license-MIT-brightgreen.svg?style=flat)](LICENSE)

JumpAccess 是一个面向 [JumpServer](https://github.com/jumpserver/jumpserver)（参考版本 `v4.1.6`）的独立访问工具项目。旨在接入 JumpServer 的环境下，尽可能地维持 SSH CLI / 第三方终端客户端的原生使用体验。

项目包含两个入口程序，复用同一套底层：
- 纯 Golang 编写的命令行程序 `jumpctl`
- 桌面 GUI，基于 Wails 2 + React.js

本项目**由 AI Agent 编码实现**，开发者负责提需求、产品设计、确定业务逻辑与边界条件、验收测试，但通常不直接阅读和修改源码。

## 项目目标

- 不依赖 JumpServer 桌面 Client，独立完成浏览器 OAuth 登录和 Token 生命周期管理。
- 以 SSH 客户端方式直接连接远程 Asset。
- 作为通用 SSH `ProxyCommand` 被兼容的终端或 SSH 客户端调用，不与某个具体终端产品耦合。
- 通过 TOML 管理多个 JumpServer Profile 以及 Asset 别名等非敏感配置。
- 支持 Windows 和 macOS，并以仅当前用户可访问的 Profile 独立文件保存 OAuth Token。

首个兼容性参考基线是 JumpServer Client `v4.1.6` 所使用的协议和接口。它是分析参考，不表示本项目依赖该桌面 Client 才能运行。

## 安装

## 安装 CLI jumpctl

需要 Go 1.25 或更高版本。

使用 Go 安装最新发布版本：

```powershell
go install github.com/cmstar/jumpaccess/cmd/jumpctl@latest
```

如果 clone 了源码，可直接从源码安装：

```powershell
go install ./cmd/jumpctl
```

若没有 Go 环境，可以从发布页直接下载，参考下面的“下载 GUI 客户端”章节。下载后，若需要全局可用，需自行添加到 `PATH` 环境变量。

验证安装：

```
jumpctl version
```

## 下载 GUI 客户端 jumpaccess

在 [发布页](https://github.com/cmstar/jumpaccess/releases) 下载对应版本的客户端。
- Windows 提供 X64 版本。
- macOS（darwin）提供 X64 和 ARM 版。

## 本地开发、测试与构建

GUI 额外需要：
- Node.js 24
- [Wails 2](https://wails.io/)

安装 Wails 2 CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails
wails doctor
```

如果仅做一次性构建，也可以不安装 Wails CLI，直接使用 `go run "github.com/wailsapp/wails/v2/cmd/wails"` 代替 `wails` 命令。

### 构建 CLI

本地测试和运行：

```
go test ./...
go vet ./...
go run ./cmd/jumpctl
```

构建 CLI：

```powershell
go build -trimpath ./cmd/jumpctl
```

### 构建 GUI

安装前端依赖：

```powershell
cd cmd/jumpaccess
npm --prefix frontend ci
npm --prefix frontend test
```

本地运行（仍在 `cmd/jumpaccess` 目录）：

```
wails dev
```

构建：

```
wails build
```

构建结果默认输出在 `cmd/jumpaccess/build/bin` 目录，单一可执行文件。

## 使用 jumpctl

完整命令和参数见 [CLI 命令参考](docs/cli.md)，也可以运行 `jumpctl --help` 或 `jumpctl <command> --help` 查看。

通用 OpenSSH 配置示例（`web` 可以是已绑定唯一 Asset 和 Account 的 Alias）：

```sshconfig
Host production-web
    HostName web
    User jumpaccess
    ProxyCommand jumpctl proxy %h
```

`auth login` 默认使用手工回调：浏览器完成授权后，不要点击确认页的“确认”，而是复制页面中的 `jms://` 链接或浏览器地址栏的完整确认页 URL，粘贴到等待中的终端。`--manual` 可显式固定这一行为，供官方客户端仍占用协议或系统不允许注册协议时使用。

桌面 GUI 只要保持运行，就会按配置周期检查所有已保存 Refresh Token 的 Profile，并在 Access Token 临近过期时自动刷新；运行期间新增登录无需重启 GUI。Refresh Token 已过期或被撤销时仍需重新登录。

`proxy` 模式不打开浏览器，也不提示选择 Account。缺少登录、Refresh Token 失效、目标或 Account 不唯一、上游主机尚未信任时，进程会在 SSH banner 之前失败，只向 stderr 写入可操作错误并返回非零状态。先运行 `jumpctl auth login` 完成授权；未知上游 gateway 需要先用 `jumpctl ssh` 进行一次人工指纹确认。

## SSH 终端内上传和下载（ZMODEM）

桌面客户端支持在 SSH 会话中使用 `rz` / `sz`，远端需安装 `lrzsz`，本机不需要额外安装这两个命令。

- 连接后在同一 SSH 连接的独立命令通道执行 `command -v rz`、`command -v sz`（最多等待 5 秒）；查到哪个命令，就启用对应的上传或下载按钮。检测中、未找到或探测失败时按钮保持可见并置灰，手工命令握手仍可启用对应能力。探测只检查命令可用性，不保证传输授权。
- SSH Tab 工具栏右侧依次为分隔线、SFTP、上传、下载、分隔线、断开连接。三个文件操作按钮始终显示，不可用时禁用，并在悬停提示中标明“当前不可用”；断开连接始终位于最右侧。
- 上传：在远端进入目标目录，点击上传按钮，或直接运行 `rz`，然后在系统对话框中选择本地文件。
- 下载：点击下载按钮输入单个远程文件路径，或直接运行 `sz file1 file2`，然后选择本地保存文件夹。同名文件自动另存为带数字后缀的文件。
- 点击按钮前，请确保终端处于命令提示符；目录应先打包再传输。下载路径按字面量处理，不展开 `~`、通配符或 Shell 表达式。
- 传输期间显示文件名和进度，可取消；普通终端输入暂时停用，切换 Tab 不影响传输。取消、错误或断连会清理未完成的本地下载。

能力检测使用独立 SSH `exec` channel。部分 JumpServer 网关会拒绝它，此时显示“无法检测”，不会向当前 Shell 插入探测命令。仍可手工运行 `rz` / `sz`，识别到握手后会弹出文件选择器并启用对应方向的按钮。命令存在不代表堡垒机允许传输；被策略阻止或超时会显示错误。

ZMODEM 使用当前 SSH 会话，不依赖 SFTP 权限。`jumpctl` CLI 和通用 `ProxyCommand` 保持原有数据透传行为，外部终端需自行支持 ZMODEM。

## 文档索引

- [Agent 工作入口](AGENTS.md)
- [架构说明](docs/architecture.md)
- [业务说明](docs/domain.md)
- [开发说明](docs/development.md)
- [CLI 命令参考](docs/cli.md)

## 自动 Release

推送符合 `vX.Y.Z` 或 `vX.Y.Z-prerelease` 的 Git tag 会触发 [Release 工作流](.github/workflows/release.yml)。

```powershell
git tag v0.1.0
git push origin v0.1.0
```

工作流执行成功会自动生成一个 Release，并根据当前版本与上一版本（tag）的差异，生成发布说明。

产物包括 Windows 和 macOS（amd64+arm64）的 CLI/GUI 程序，每个都可以独立下载。

## 安全说明

不要把账号密码、Access Token、Refresh Token、Cookie、私钥或其他真实凭据写入仓库。真实 JumpServer 账号只用于开发者本机的手工 smoke test；常规自动化测试应使用模拟服务和脱敏 fixture。

OAuth 凭据以每个 Profile 一个 JSON 文件的方式保存在应用目录的 `credentials` 子目录。文件名由 Profile 标识稳定派生，不直接使用或改写 Profile 名；Windows 使用仅当前用户和 `SYSTEM` 可访问的受保护 DACL，macOS 使用当前用户所有的 `0700` 目录和 `0600` 文件。文件内容包含 Access Token 和 Refresh Token，应像 SSH 私钥一样保护，不能复制、同步或提交到仓库。

Windows Credential Manager 或 macOS Keychain 仅用于保存 ProxyCommand façade 的稳定 Ed25519 host key，不参与 OAuth Token 的读取或写入；包含 macOS Keychain 后端的正式构建需要启用 CGO 并链接系统 Security framework。

首次直接连接某个 JumpServer SSH gateway 时，`jumpctl ssh` 会显示 SHA-256 主机密钥指纹并要求明确确认；信任记录保存在同一 JumpAccess 应用目录的 `known_hosts`。主机密钥变化不会自动接受。

ProxyCommand 存在两层独立的主机信任：外部 SSH 客户端看到的是 JumpAccess 本地 façade 的稳定 Ed25519 host key，该私钥保存在操作系统凭据存储；JumpAccess 自己仍使用上述 `known_hosts` 严格验证上游 JumpServer gateway。
