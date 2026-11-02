# 版本发布规范

## 适用范围

本规范适用于 JumpAccess 的正式版和所有非正式版，约定发布说明、比较基准，以及维护者或 Agent 准备和推送版本的步骤。[自动发布](../development.md#自动发布)说明构建矩阵与工作流权限；逐版本正文保存在 [docs/releases/](../releases/README.md)。

GitHub Actions 继续负责验证、构建、归档、校验值和 GitHub Release。Agent 在推送标签前整理发布说明，工作流不调用模型生成文案。

## 版本与比较基准

- 正式版标签为 `vX.Y.Z`。
- 非正式版标签为 `vX.Y.Z-<suffix>`，尾缀按语义版本规则由点分隔的非空标识符组成，允许 ASCII 字母、数字和连字符；纯数字标识符不得有多余的前导零。`pre`、`alpha`、`beta`、`rc`、`nightly` 和合法自定义尾缀均属于非正式版，不为某个尾缀设置专用规则。
- `X`、`Y`、`Z` 为无多余前导零的非负整数；当前不接受 `+build` 元数据。版本比较使用语义版本优先级，不使用字符串排序、标签创建时间或提交距离。
- GitHub Release 的标题使用完整标签。正式版不标记 `prerelease`；所有非正式版均标记 `prerelease`。

比较基准只从已成功发布、非草稿的 GitHub Releases 选择，同时要求标签可在本地解析、其提交在目标提交历史中可达，且语义版本严格低于目标。标签存在但没有成功发布 Release、草稿、不可达分支和高于或等于目标的版本均不能作为基准。

| 目标版本 | 选择规则 |
| --- | --- |
| 正式版 | 最高的较低正式版 |
| 非正式版 | 相同 `X.Y.Z` 中最高的较低非正式版；没有时选择最高的较低正式版 |
| 没有符合条件的版本 | 使用截至目标提交的完整历史 |

例如 `v0.2.0` 对比上一正式版 `v0.1.8`；`v0.2.0-pre.3` 优先对比已发布的 `v0.2.0-pre.2`；若 `v0.2.0-alpha.1` 是该系列第一个非正式版，则对比 `v0.1.8`。不同非正式尾缀仍按语义版本优先级比较，不按 `alpha → beta → rc → pre` 等自定义阶段表排序。

Agent 与 workflow 都必须使用 `scripts/release-notes.mjs` 的共同规则确定基准。不要另写一套标签选择命令，也不要复用未经刷新的旧 Release 清单。

## 正文来源与编写要求

正文文件为 `docs/releases/<完整标签>.md`，例如 `docs/releases/v0.2.0-rc.1.md`。默认从目标标签或 `--ref` 指定提交的 Git 树中读取，工作区里同名的未提交修改不会自动进入发布说明。

1. 文件存在且非空白时，直接使用该 Markdown 正文。
2. 文件不存在时输出警告，并按同一比较基准回退到 Conventional Commits 分类说明，保留缺少逐版本正文时的发布兼容性。
3. 文件存在但只有空白时直接报错，不把未完成的正文当作缺少文件，不静默回退。

正文面向该比较基准的升级用户，按实际需要说明新功能、用户可感知的改进、既有问题修复、兼容性变化和已知限制。每条说明表达最终行为及其影响；以代码、真实 diff、测试和当前文档核对事实，提交日志只作为线索。

同一版本中新增功能及其开发期间的多次修正合并成一条最终功能说明。只影响测试、重构或开发工具且没有用户可见结果的提交通常不单列；已经出现在比较基准版本中的问题被修复时，再作为修复说明。不得将尚未实现的设计、跳过的检查或未经验证的兼容性写成已完成能力。

正文只写本版内容，不复制统一的下载说明、校验值文件说明、签名提醒和完整变更链接。这些内容由发布脚本追加，确保文件名与目标标签一致、比较链接与基准一致。没有基准时链接到完整提交历史。

## 本机预览

发布脚本的共同入口为：

```text
node scripts/release-notes.mjs --tag <tag> --repository <owner/repo> --releases-file <JSON> --output <file>
```

`--releases-file` 接收 GitHub Releases API 的 JSON 数组，也接受 `gh api --paginate --slurp` 产生的分页数组。清单必须包含全部分页；获取失败时停止处理，不以空数组冒充没有历史版本。可以使用 GitHub CLI 或等效 API 客户端；本机没有 `gh` 时不要求为此更换现有工具。

下面的 PowerShell 示例仅刷新远端信息并预览，不创建或推送标签。`$releaseTag` 改为本次准备的目标版本，`$releaseRepository` 与 Git remote 对应；`origin` 应替换为实际发布远端：

```powershell
$releaseTag = 'v0.2.0-rc.1'
$releaseRepository = 'cmstar/jumpaccess'
$releasePreviewDir = Join-Path ([IO.Path]::GetTempPath()) ('jumpaccess-release-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $releasePreviewDir | Out-Null
git fetch origin --tags
if ($LASTEXITCODE -ne 0) { throw '刷新远端标签失败' }
$releaseJSON = gh api --paginate --slurp "repos/$releaseRepository/releases?per_page=100"
if ($LASTEXITCODE -ne 0) { throw '读取已发布版本失败' }
$releaseListPath = Join-Path $releasePreviewDir 'releases.json'
[IO.File]::WriteAllText($releaseListPath, ($releaseJSON -join "`n"), [Text.UTF8Encoding]::new($false))
node scripts/release-notes.mjs --tag $releaseTag --ref HEAD --repository $releaseRepository --releases-file $releaseListPath --output (Join-Path $releasePreviewDir 'release-notes.md')
if ($LASTEXITCODE -ne 0) { throw '生成发布说明预览失败' }
```

尚未创建标签时，`--ref HEAD` 表示用当前提交作为目标代码。脚本会输出比较基准和正文来源；初次运行据此确认基准，并从预览末尾的完整变更链接核对范围。若对应正文尚未提交，此时出现缺少文件的回退警告属于预览的预期结果。正文写好后加上 `--notes-file "docs/releases/$releaseTag.md"` 预览未提交正文；该参数是显式的本机预览覆盖，不改变发布工作流从目标提交读取的默认行为，显式指定的文件不存在、无法读取或为空白时均报错。已有目标标签时省略 `--ref`，提交后的正文也应省略 `--notes-file` 再次核对。

未提交的产品代码不包含在 `HEAD` 的差异中；编写说明时还须审查本次范围内的工作区 diff，相关代码提交后再按最终提交核对。API 清单和合成后的 `release-notes.md` 均写入系统临时目录，不加入 Git；只提交经过审阅的逐版本正文源文件。

## Agent 执行约定

用户明确要求“在当前版本上创建并推送 `v...`”、发布指定版本或同义操作时，该指令已授权准备说明、执行必要验证、提交本次范围、创建并推送目标标签，以及检查发布工作流。按下列步骤完成，不重复询问是否发布；用户仅要求准备、编写或预览时，停在相应准备结果，不推送标签。

1. 检查当前分支、HEAD、工作区、远端和目标版本格式，确认本次发布范围。保留无关未提交内容，不执行一揽子 `git add -A`；当前范围确有歧义时才澄清。检查远端标签和 Release 是否已存在：出现冲突时说明现状，不自动删除、移动标签、覆盖 Release 或强推。
2. 刷新远端标签及完整的已发布 Release 清单，用共同脚本确认比较基准。阅读基准到目标的真实代码差异、相关文档与本次范围内尚未提交的改动，按最终用户可见结果编写对应正文。
3. 将正文与脚本统一追加的内容一起输出到临时目录预览，核对版本、基准、正文事实、下载文件名、完整变更链接、兼容性和已知限制；运行与本次改动相关的检查，并满足[当前验证入口](../development.md#当前验证入口)的发布验证要求。未验证项如实说明，失败时先定位和修复。
4. 按既有提交约定仅提交本次范围内的变更和正文，保留无关文件。发布正文必须在将要打标签的提交中；在最终提交上重新预览，核对正文与代码一致，再创建用户指定的标签并推送该标签到确认的发布远端。
5. 关注该标签触发的 Release workflow 直至成功或出现明确失败。发布完成后核对 Release 的正式／非正式标记、正文和附件，再提供 Release 链接。失败时说明具体阶段及后续处理；不能把标签推送成功称为版本发布成功，也不得为重试自动覆盖已有标签或 Release。

以上授权仅适用于用户指定版本和本次范围，不扩大为清理其他分支、删除历史版本、修改已发布内容或自动发布后续版本的授权。已发布版本的修订需单独明确范围。

## 验证与维护

修改选择规则、正文来源或工作流入口时，补充并运行 `node --test scripts/*.test.mjs`，覆盖正式版与不同非正式尾缀、Release 状态、祖先关系、版本顺序、无基准、正文存在／缺失／空白，以及本机预览与默认读取的差异。工作流的现有应用测试、跨平台验证和构建要求不因文案机制改变而降低。

规则变化时同步维护本文、[开发说明](../development.md#自动发布)、[规范索引](README.md)、[发布说明目录](../releases/README.md)及 [AGENTS.md](../../AGENTS.md) 中的入口和授权边界。
