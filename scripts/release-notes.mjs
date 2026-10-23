import { execFileSync } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const conventionalCommit = /^(?<type>feat|fix|perf|refactor|docs|build|ci|test|chore)(?:\((?<scope>[^)]+)\))?(?<breaking>!)?:\s*(?<description>.+)$/u

const categories = [
  { key: 'breaking', title: '破坏性变更' },
  { key: 'feat', title: '新功能' },
  { key: 'fix', title: '问题修复' },
  { key: 'perf', title: '性能改进' },
  { key: 'refactor', title: '内部调整' },
  { key: 'docs', title: '文档更新' },
  { key: 'maintenance', title: '工程维护' },
  { key: 'other', title: '其他变更' },
]

export function parseCommit(commit) {
  const match = conventionalCommit.exec(commit.subject)
  if (!match?.groups) {
    return {
      breaking: /^BREAKING(?: |-)CHANGE:/mu.test(commit.body),
      description: commit.subject,
      hash: commit.hash,
      scope: '',
      type: 'other',
    }
  }

  return {
    breaking: Boolean(match.groups.breaking) || /^BREAKING(?: |-)CHANGE:/mu.test(commit.body),
    description: match.groups.description,
    hash: commit.hash,
    scope: match.groups.scope ?? '',
    type: match.groups.type,
  }
}

function categoryFor(commit) {
  if (commit.breaking) {
    return 'breaking'
  }
  if (['build', 'ci', 'test', 'chore'].includes(commit.type)) {
    return 'maintenance'
  }
  return commit.type
}

function renderCommit(commit, repository) {
  const scope = commit.scope ? `**${commit.scope}**：` : ''
  const shortHash = commit.hash.slice(0, 7)
  const commitURL = `https://github.com/${repository}/commit/${commit.hash}`
  return `- ${scope}${commit.description} ([${shortHash}](${commitURL}))`
}

function renderCommitBody(commits, repository) {
  const grouped = new Map(categories.map(({ key }) => [key, []]))
  for (const rawCommit of commits) {
    const commit = parseCommit(rawCommit)
    const category = categoryFor(commit)
    grouped.get(category)?.push(commit)
  }

  const lines = []
  let renderedCommitCount = 0
  for (const { key, title } of categories) {
    const entries = grouped.get(key)
    if (!entries?.length) {
      continue
    }
    renderedCommitCount += entries.length
    lines.push(`## ${title}`, '')
    lines.push(...entries.map((commit) => renderCommit(commit, repository)), '')
  }

  if (renderedCommitCount === 0) {
    lines.push('## 更新内容', '', '本版本没有可列出的提交。', '')
  }

  return lines.join('\n').trimEnd()
}

export function renderReleaseNotes({ commits = [], body, previousTag, repository, tag }) {
  const lines = [body?.trim() ?? renderCommitBody(commits, repository), '']

  lines.push(
    '## 下载说明',
    '',
    `- \`jumpctl-${tag}-windows-amd64.zip\`：Windows CLI`,
    `- \`jumpaccess-${tag}-windows-amd64.zip\`：Windows 桌面 GUI`,
    `- \`jumpctl-${tag}-darwin-amd64.tar.gz\`：macOS Intel CLI`,
    `- \`jumpctl-${tag}-darwin-arm64.tar.gz\`：macOS Apple Silicon CLI`,
    `- \`jumpaccess-${tag}-darwin-universal.zip\`：macOS Universal 桌面 GUI`,
    '- `checksums.txt`：所有发布文件的 SHA-256 校验值',
    '',
    '> 可执行文件没有进行 Windows/macOS 签名，新下载的文件首次运行时操作系统可能显示安全提醒，放行后可正常使用，后续不会再次提示。',
    '',
    '## 完整变更',
    '',
  )

  if (previousTag) {
    lines.push(`[${previousTag}...${tag}](https://github.com/${repository}/compare/${previousTag}...${tag})`)
  } else {
    lines.push(`[查看截至 ${tag} 的提交历史](https://github.com/${repository}/commits/${tag})`)
  }

  return `${lines.join('\n').trimEnd()}\n`
}

function git(args) {
  return execFileSync('git', args, { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 }).trim()
}

export function parseVersion(tag) {
  const match = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/u.exec(tag ?? '')
  if (!match) {
    return null
  }
  const suffix = match[4]?.split('.') ?? []
  if (suffix.some((identifier) => /^0\d+$/u.test(identifier))) {
    return null
  }
  return { tag, core: match.slice(1, 4).map(BigInt), suffix }
}

function compareNumber(left, right) {
  return left < right ? -1 : left > right ? 1 : 0
}

function compareVersions(left, right) {
  for (let index = 0; index < 3; index += 1) {
    const compared = compareNumber(left.core[index], right.core[index])
    if (compared !== 0) return compared
  }
  if (!left.suffix.length || !right.suffix.length) {
    return compareNumber(Number(!left.suffix.length), Number(!right.suffix.length))
  }
  for (let index = 0; index < Math.min(left.suffix.length, right.suffix.length); index += 1) {
    const a = left.suffix[index]
    const b = right.suffix[index]
    if (a === b) continue
    const numericA = /^\d+$/u.test(a)
    const numericB = /^\d+$/u.test(b)
    if (numericA && numericB) return compareNumber(BigInt(a), BigInt(b))
    if (numericA !== numericB) return numericA ? -1 : 1
    return a < b ? -1 : 1
  }
  return compareNumber(left.suffix.length, right.suffix.length)
}

async function findPreviousTag(version, ref, releasesFile) {
  const pages = JSON.parse((await readFile(releasesFile, 'utf8')).replace(/^\uFEFF/u, ''))
  if (!Array.isArray(pages)) {
    throw new Error('--releases-file 必须是 GitHub Releases 数组或分页数组')
  }
  const releases = pages.flat()
  if (releases.some((release) => !release || typeof release.tag_name !== 'string'
    || typeof release.draft !== 'boolean' || !Object.hasOwn(release, 'published_at'))) {
    throw new Error('--releases-file 包含无效的 GitHub Release 记录')
  }
  const published = new Set(releases
    .filter((release) => !release.draft && release.published_at)
    .map((release) => release.tag_name))
  // 从目标提交本身查可达标签，保留同一提交上先后发布的版本。
  const candidates = git(['tag', '--merged', ref, '--list', 'v*'])
    .split(/\r?\n/u)
    .filter((tag) => published.has(tag))
    .map(parseVersion)
    .filter((candidate) => candidate && compareVersions(candidate, version) < 0)
    .sort((left, right) => compareVersions(right, left))
  if (version.suffix.length) {
    const previousPreview = candidates.find((candidate) => candidate.suffix.length
      && candidate.core.every((part, index) => part === version.core[index]))
    if (previousPreview) return previousPreview.tag
  }
  return candidates.find((candidate) => !candidate.suffix.length)?.tag ?? null
}

async function readNotesBody(tag, ref, notesFile) {
  const source = notesFile ?? `docs/releases/${tag}.md`
  let body
  if (notesFile) {
    body = await readFile(notesFile, 'utf8')
  } else {
    const entry = git(['ls-tree', ref, '--', source])
    if (!entry) {
      const message = `未找到 ${source}，回退到提交日志自动生成发布说明。`
      console.warn(`${process.env.GITHUB_ACTIONS === 'true' ? '::warning::' : ''}${message}`)
      return undefined
    }
    if (!/^100(?:644|755) blob /u.test(entry)) {
      throw new Error(`发布说明必须是普通文件：${source}`)
    }
    body = git(['show', `${ref}:${source}`])
  }
  if (!body.trim()) {
    throw new Error(`发布说明文件为空：${source}`)
  }
  console.log(`发布说明来源：${source}`)
  return body
}

function readCommits(ref, previousTag) {
  const range = previousTag ? `refs/tags/${previousTag}..${ref}` : ref
  const output = git([
    'log',
    '--no-merges',
    '--format=%H%x1f%s%x1f%b%x1e',
    range,
  ])
  if (!output) {
    return []
  }

  return output
    .split('\x1e')
    .map((record) => record.trim())
    .filter(Boolean)
    .map((record) => {
      const [hash = '', subject = '', body = ''] = record.split('\x1f')
      return { hash: hash.trim(), subject: subject.trim(), body: body.trim() }
    })
}

function readArguments(argv) {
  const values = new Map()
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index]
    const value = argv[index + 1]
    if (!key?.startsWith('--') || value === undefined) {
      throw new Error(`无效参数：${key ?? ''}`)
    }
    values.set(key.slice(2), value)
  }
  return values
}

async function main() {
  const argumentsMap = readArguments(process.argv.slice(2))
  const tag = argumentsMap.get('tag')
  const repository = argumentsMap.get('repository')
  const output = argumentsMap.get('output')
  const releasesFile = argumentsMap.get('releases-file')
  const version = parseVersion(tag)

  if (!version) {
    throw new Error('tag 必须符合 vX.Y.Z 或 vX.Y.Z-prerelease')
  }
  if (!repository || !/^[^/]+\/[^/]+$/u.test(repository)) {
    throw new Error('repository 必须符合 owner/name')
  }
  if (!output) {
    throw new Error('必须指定 --output')
  }
  if (!releasesFile) {
    throw new Error('必须指定 --releases-file，使用 GitHub 已发布版本确定比较基准')
  }

  const target = argumentsMap.get('ref') ?? `refs/tags/${tag}`
  const ref = git(['rev-parse', '--verify', '--end-of-options', `${target}^{commit}`])
  const previousTag = await findPreviousTag(version, ref, releasesFile)
  console.log(`比较基准：${previousTag ?? '无（使用完整历史）'}`)
  const body = await readNotesBody(tag, ref, argumentsMap.get('notes-file'))
  const commits = body === undefined ? readCommits(ref, previousTag) : []
  const notes = renderReleaseNotes({ commits, body, previousTag, repository, tag })
  await writeFile(output, notes, 'utf8')
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : ''
if (import.meta.url === entryPoint) {
  main().catch((error) => {
    console.error(error.message)
    process.exitCode = 1
  })
}
