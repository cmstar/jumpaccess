# JumpAccess 用户指南

本目录面向使用 JumpAccess 访问 JumpServer 资产的用户，按使用场景介绍操作步骤。

- [理解数据层级：Profile、组织、资产与账号](data-hierarchy.md)：先了解站点、本地配置、资源和登录身份之间的关系，以及别名保存了什么。
- [CLI 入门：从安装到第一次 SSH 连接](cli-quick-start.md)：安装 `jumpctl`，完成浏览器认证，依次查询组织、资产和账号，绑定别名，再选择 `jssh` 快捷命令或 OpenSSH 的 `ssh` 命令连接。
- [SSH 终端内上传和下载文件](file-transfer.md)：使用桌面客户端或 `jumpctl ssh` 的 `rz` / `sz` 传输文件，设置下载目录，查看进度并处理常见问题。
- [CLI 命令参考](../cli.md)：查阅完整命令和参数。
- [在 GUI 中使用 MCP Server](agent-mcp.md)：配置并手动或随应用自动启动服务，让 Agent 下载日志与目录，查看共享连接与文本日志，处理停止、重启和结果过期。

[返回项目首页](../../README.md)
