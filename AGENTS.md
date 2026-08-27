# Agent 工作入口

JumpAccess 的首个交付物是 Go CLI `jumpctl`，用于独立完成 JumpServer OAuth 认证，并提供直接 SSH 和通用 SSH `ProxyCommand` 能力。项目当前处于初始化阶段；文档中的目标设计不得当作已实现功能。

## 按需阅读

- 项目定位和当前状态：`README.md`
- 系统边界、模块关系或技术选择：`docs/architecture.md`
- Profile、Organization、Asset、Account、Alias、Token 和 Session 规则：`docs/domain.md`
- 工程布局、测试、安全和跨平台约束：`docs/development.md`

## Agent 工作规则

- 默认使用中文编写文档、说明、提交信息、注释和开发沟通；代码标识符、命令、文件名、包名、API、错误码和第三方专有名词保持原文。
- 保持修改小而聚焦；新增抽象前优先复用已有模式。
- 区分目标设计与当前实现，结论以当前代码和测试为准。
- 修改架构、领域规则、对外 CLI 契约或开发流程时，同步更新对应文档。
- 生产行为变更采用 RED–GREEN–REFACTOR，并在结束前运行与修改最相关的检查。
- 不把产品能力写成 Tabby 专用能力；`ProxyCommand` 面向所有兼容客户端。
- 不提交密码、Token、Cookie、私钥、生产数据或含秘密的测试输出；真实账号只用于本机手工 smoke test。
- 未经明确确认，不执行破坏性数据操作、迁移或发布。
- 受保护内容：当前尚无证据表明存在项目特有的生成物或禁止改动目录；工程建立后应据实补充。
