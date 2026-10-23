import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const workspace = fileURLToPath(new URL('..', import.meta.url))
const workflow = readFileSync(new URL('../.github/workflows/release.yml', import.meta.url), 'utf8')
const gitPath = process.platform === 'win32'
  ? spawnSync('where.exe', ['git'], { encoding: 'utf8' }).stdout.trim().split(/\r?\n/)[0]
  : null
const bash = process.env.JUMPACCESS_TEST_BASH ?? (gitPath
  ? path.resolve(path.dirname(gitPath), '../bin/bash.exe') : '/bin/bash')

function stepScript(name) {
  const step = workflow.split(`      - name: ${name}`)[1]?.split(/\r?\n      - name:/u)[0]
  assert.ok(step, `缺少工作流步骤：${name}`)
  const script = step.split('run: |')[1]
  assert.ok(script, `步骤没有内联脚本：${name}`)
  return script.trimEnd().split(/\r?\n/).slice(1)
    .map(line => line.replace(/^ {10}/u, '')).join('\n')
}

function temporaryDirectory(t) {
  const parent = path.resolve(os.tmpdir())
  const directory = mkdtempSync(path.join(parent, 'jumpaccess-release-workflow-'))
  t.after(() => {
    assert.equal(path.dirname(directory), parent)
    rmSync(directory, { recursive: true, force: true })
  })
  return directory
}

function runScript(script, env, cwd = workspace) {
  const result = spawnSync(bash, ['--noprofile', '--norc', '-c', script], {
    cwd,
    encoding: 'utf8',
    env: { ...process.env, ...env },
  })
  assert.ifError(result.error)
  return result
}

test('发布工作流保留任意合法非正式尾缀，并为桌面元数据提取纯版本号', (t) => {
  const directory = temporaryDirectory(t)
  for (const tag of ['v2.0.0', 'v2.0.0-preview.1', 'v2.0.0-nightly.20261024', 'v2.0.0-test-foo']) {
    const output = path.join(directory, `${tag}.txt`)
    const result = runScript(stepScript('验证并解析版本'), {
      GITHUB_REF_NAME: tag,
      GITHUB_OUTPUT: output,
    })
    assert.equal(result.status, 0, `${tag}: ${result.stderr}`)
    assert.equal(readFileSync(output, 'utf8'), `version=${tag.slice(1)}\nmetadata_version=2.0.0\n`)
  }
})

test('发布工作流拒绝无效标签，且不写入版本输出', (t) => {
  const directory = temporaryDirectory(t)
  const invalidTags = ['v2.0.0-', 'v2.0.0-preview..1', 'v02.0.0', 'v2.0.0-rc.01', 'v2.0.0+build']
  for (const [index, tag] of invalidTags.entries()) {
    const output = path.join(directory, `invalid-${index}.txt`)
    const result = runScript(stepScript('验证并解析版本'), {
      GITHUB_REF_NAME: tag,
      GITHUB_OUTPUT: output,
    })
    assert.notEqual(result.status, 0, tag)
    assert.match(result.stderr, /发布标签必须符合/)
    assert.equal(existsSync(output), false, tag)
  }
})

test('读取 GitHub 发布历史失败时工作流停止，不生成可能选错基准的说明', (t) => {
  const directory = temporaryDirectory(t)
  const script = [
    'gh() { return 42; }',
    'node() { printf "called" > "$RUNNER_TEMP/node-called"; }',
    stepScript('生成 Release Notes'),
  ].join('\n')
  const result = runScript(script, {
    RUNNER_TEMP: directory.replaceAll('\\', '/'),
    GITHUB_REF_NAME: 'v2.0.0',
    GITHUB_REPOSITORY: 'cmstar/jumpaccess',
  }, directory)
  assert.equal(result.status, 42, result.stderr)
  assert.equal(existsSync(path.join(directory, 'node-called')), false)
  assert.equal(existsSync(path.join(directory, 'release-notes.md')), false)
})
