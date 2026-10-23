import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const script = fileURLToPath(new URL('./release-notes.mjs', import.meta.url))

function published(tag, overrides = {}) {
  return {
    tag_name: tag,
    draft: false,
    prerelease: tag.includes('-'),
    published_at: '2026-10-01T00:00:00Z',
    ...overrides,
  }
}

function repository(t) {
  const temporaryDirectory = path.resolve(os.tmpdir())
  const directory = mkdtempSync(path.join(temporaryDirectory, 'jumpaccess-release-notes-'))
  t.after(() => {
    assert.equal(path.dirname(directory), temporaryDirectory)
    rmSync(directory, { recursive: true, force: true })
  })
  const git = (...args) => execFileSync('git', [
    '-c', 'user.name=Release Notes Test',
    '-c', 'user.email=release-notes@example.invalid',
    '-c', 'commit.gpgsign=false',
    '-c', 'tag.gpgsign=false',
    '-c', `core.hooksPath=${path.join(directory, 'no-hooks')}`,
    ...args,
  ], { cwd: directory, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim()
  git('init', '--quiet', '--initial-branch=main')
  let sequence = 0
  return {
    directory,
    git,
    commit(subject, ...tags) {
      git('commit', '--quiet', '--allow-empty', '-m', subject)
      for (const tag of tags) git('tag', tag)
      return git('rev-parse', 'HEAD')
    },
    writeNotes(tag, contents) {
      const relativePath = `docs/releases/${tag}.md`
      const filename = path.join(directory, relativePath)
      mkdirSync(path.dirname(filename), { recursive: true })
      writeFileSync(filename, contents, 'utf8')
      return relativePath
    },
    run(tag, releases, extraArguments = []) {
      sequence += 1
      const releasesFile = path.join(directory, `releases-${sequence}.json`)
      const output = path.join(directory, `output-${sequence}.md`)
      writeFileSync(releasesFile, JSON.stringify(releases), 'utf8')
      const result = spawnSync(process.execPath, [
        script, '--tag', tag, '--repository', 'cmstar/jumpaccess',
        '--releases-file', releasesFile, '--output', output, ...extraArguments,
      ], { cwd: directory, encoding: 'utf8' })
      assert.ifError(result.error)
      return {
        status: result.status,
        diagnostics: `${result.stdout}\n${result.stderr}`,
        notes: existsSync(output) ? readFileSync(output, 'utf8') : '',
      }
    },
  }
}

function successful(result) {
  assert.equal(result.status, 0, result.diagnostics)
  return result.notes
}

test('正式版从已发布且可达的较低正式标签中选最高版本，并提示提交日志回退', (t) => {
  const repo = repository(t)
  repo.commit('feat: 初始功能', 'v1.1.0')
  const base = repo.commit('feat: 上个正式版功能', 'v1.2.0')
  repo.git('checkout', '--quiet', '-b', 'unmerged')
  repo.commit('feat: 尚未合入的功能', 'v1.10.0')
  repo.git('checkout', '--quiet', 'main')
  repo.commit('feat: 本轮完整功能', 'v2.0.0-rc.1')
  repo.commit('test: 未发布标签所在提交', 'v1.7.0', 'v1.8.0', 'v1.9.0', 'v3.0.0')
  repo.commit('fix: 完成功能修正', 'v2.0.0')
  const result = repo.run('v2.0.0', [[
    published('v1.9.0', { draft: true }),
    published('v2.0.0-rc.1', { prerelease: false }),
    published('v1.2.0', { prerelease: true }),
  ], [
    published('v1.1.0', { published_at: '2026-10-23T00:00:00Z' }),
    published('v1.8.0', { published_at: null }),
    published('v1.10.0'),
    published('v1.11.0'),
    published('v3.0.0'),
  ]])
  const notes = successful(result)
  assert.match(notes, /compare\/v1\.2\.0\.\.\.v2\.0\.0/)
  assert.match(notes, /本轮完整功能/)
  assert.match(notes, /完成功能修正/)
  assert.doesNotMatch(notes, /上个正式版功能|尚未合入的功能/)
  assert.doesNotMatch(notes, new RegExp(`/commit/${base}`))
  assert.match(result.diagnostics, /回退/)
})

test('非正式版按同系列语义版本排序，支持任意尾缀和同提交标签', (t) => {
  const repo = repository(t)
  repo.commit('feat: 已发布正式功能', 'v1.0.0')
  repo.commit('feat: 已发布预览功能', 'v2.0.0-alpha.1', 'v2.0.0-beta.1', 'v2.0.0-rc.2')
  repo.git('checkout', '--quiet', '-b', 'unmerged')
  repo.commit('feat: 未合入预览功能', 'v2.0.0-rc.10.5')
  repo.git('checkout', '--quiet', 'main')
  repo.commit('fix: 已发布候选版本修正', 'v2.0.0-rc.10', 'v2.0.0-rc.11', 'v2.0.0-rc.12', 'v2.0.0', 'v1.9.0-nightly.1')
  const notes = successful(repo.run('v2.0.0-rc.11', [
    published('v2.0.0-rc.2'), published('v2.0.0-alpha.1'),
    published('v2.0.0-beta.1'), published('v2.0.0-rc.10', { prerelease: false }),
    published('v2.0.0-rc.10.5'), published('v2.0.0-rc.11'), published('v2.0.0-rc.12'),
    published('v2.0.0'), published('v1.9.0-nightly.1'), published('v1.0.0'),
  ]))
  assert.match(notes, /compare\/v2\.0\.0-rc\.10\.\.\.v2\.0\.0-rc\.11/)
  assert.match(notes, /本版本没有可列出的提交/)
  assert.doesNotMatch(notes, /已发布预览功能|已发布候选版本修正/)
})

test('首个非正式版忽略其他系列的预览版，比较上一正式版', (t) => {
  const repo = repository(t)
  repo.commit('feat: 上个正式版功能', 'v1.8.0')
  repo.commit('feat: 新版本功能', 'v1.9.0-beta.9')
  repo.commit('feat: 夜间构建功能', 'v2.0.0-nightly.20261024', 'v2.0.0-rc.1')
  const notes = successful(repo.run('v2.0.0-nightly.20261024', [
    published('v1.8.0'), published('v1.9.0-beta.9'), published('v2.0.0-rc.1'),
  ]))
  assert.match(notes, /compare\/v1\.8\.0\.\.\.v2\.0\.0-nightly\.20261024/)
  assert.match(notes, /新版本功能/)
  assert.match(notes, /夜间构建功能/)
  assert.doesNotMatch(notes, /上个正式版功能/)
})

test('没有已发布基准时保留完整提交历史', (t) => {
  const repo = repository(t)
  repo.commit('feat: 历史功能', 'v0.1.0')
  repo.commit('feat: 首次发布功能', 'v0.2.0')
  const notes = successful(repo.run('v0.2.0', []))
  assert.match(notes, /历史功能/)
  assert.match(notes, /首次发布功能/)
  assert.match(notes, /commits\/v0\.2\.0/)
  assert.doesNotMatch(notes, /\/compare\//)
})

test('读取目标标签中的说明正文，并自动保留下载说明和比较链接', (t) => {
  const repo = repository(t)
  repo.commit('feat: 已发布功能', 'v1.0.0')
  const filename = repo.writeNotes('v2.0.0', '## 本次更新\n\n面向用户的完整功能说明。\n')
  repo.git('add', '--', filename)
  repo.commit('fix: 开发过程中的临时修复', 'v2.0.0')
  repo.writeNotes('v2.0.0', '不应进入发布说明的未提交改动。\n')
  const notes = successful(repo.run('v2.0.0', [published('v1.0.0')]))
  assert.match(notes, /^## 本次更新\n\n面向用户的完整功能说明。/)
  assert.doesNotMatch(notes, /开发过程中的临时修复|不应进入发布说明的未提交改动/)
  assert.match(notes, /## 下载说明/)
  assert.match(notes, /jumpctl-v2\.0\.0-windows-amd64\.zip/)
  assert.match(notes, /checksums\.txt/)
  assert.match(notes, /compare\/v1\.0\.0\.\.\.v2\.0\.0/)
})

test('目标标签中的说明文件为空时失败，避免静默发布错误正文', (t) => {
  const repo = repository(t)
  const filename = repo.writeNotes('v1.0.0', ' \n\t\n')
  repo.git('add', '--', filename)
  repo.commit('feat: 首次功能', 'v1.0.0')
  const result = repo.run('v1.0.0', [])
  assert.notEqual(result.status, 0)
  assert.match(result.diagnostics, /空/)
  assert.equal(result.notes, '')
})

test('创建标签前可从 HEAD 预览差异，再使用尚未提交的说明文件预览正文', (t) => {
  const repo = repository(t)
  repo.commit('feat: 上个正式版功能', 'v1.0.0')
  repo.commit('feat: 待发布功能')
  const releases = [published('v1.0.0')]
  const fallback = repo.run('v2.0.0-preview.1', releases, ['--ref', 'HEAD'])
  assert.match(successful(fallback), /待发布功能/)
  assert.match(fallback.notes, /compare\/v1\.0\.0\.\.\.v2\.0\.0-preview\.1/)
  assert.match(fallback.diagnostics, /回退/)
  const filename = repo.writeNotes('v2.0.0-preview.1', '## 更新内容\n\n整理后的待发布功能。\n')
  const notes = successful(repo.run('v2.0.0-preview.1', releases, ['--ref', 'HEAD', '--notes-file', filename]))
  assert.match(notes, /整理后的待发布功能/)
  assert.doesNotMatch(notes, /\/commit\//)
  assert.match(notes, /## 下载说明/)
  assert.match(notes, /compare\/v1\.0\.0\.\.\.v2\.0\.0-preview\.1/)
  assert.equal(repo.git('tag', '--list', 'v2.0.0-preview.1'), '')
})
