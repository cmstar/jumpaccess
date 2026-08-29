import { execFileSync } from 'node:child_process'
import { writeFile } from 'node:fs/promises'
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

export function renderReleaseNotes({ commits, previousTag, repository, tag }) {
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
    '> Windows 与 macOS 桌面构建当前未使用商业代码签名证书；操作系统可能显示安全提示。',
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
  return execFileSync('git', args, { encoding: 'utf8' }).trim()
}

function findPreviousTag(tag) {
  try {
    return git(['describe', '--tags', '--abbrev=0', '--match', 'v[0-9]*', `${tag}^`]) || null
  } catch {
    return null
  }
}

function readCommits(tag, previousTag) {
  const range = previousTag ? `${previousTag}..${tag}` : tag
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

  if (!tag || !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/u.test(tag)) {
    throw new Error('tag 必须符合 vX.Y.Z 或 vX.Y.Z-prerelease')
  }
  if (!repository || !/^[^/]+\/[^/]+$/u.test(repository)) {
    throw new Error('repository 必须符合 owner/name')
  }
  if (!output) {
    throw new Error('必须指定 --output')
  }

  const previousTag = findPreviousTag(tag)
  const commits = readCommits(tag, previousTag)
  const notes = renderReleaseNotes({ commits, previousTag, repository, tag })
  await writeFile(output, notes, 'utf8')
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : ''
if (import.meta.url === entryPoint) {
  main().catch((error) => {
    console.error(error.message)
    process.exitCode = 1
  })
}
