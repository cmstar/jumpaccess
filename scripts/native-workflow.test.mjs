import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import path from 'node:path'
import test from 'node:test'

const workflow = readFileSync(new URL('../.github/workflows/native.yml', import.meta.url), 'utf8')
const step = workflow.split('- name: 构建 GUI')[1].split('- name: 标记检查失败')[0]
const script = step.split('run: |')[1].trimEnd().split(/\r?\n/)
  .filter(line => line.trim()).map(line => line.replace(/^ {10}/, '')).join('\n')
// Windows 使用 Git 自带的 Bash；CI 的 macOS 使用 /bin/bash。
const gitPath = process.platform === 'win32'
  ? spawnSync('where.exe', ['git'], { encoding: 'utf8' }).stdout.trim().split(/\r?\n/)[0]
  : null
const bash = process.env.JUMPACCESS_TEST_BASH ?? (gitPath
  ? path.resolve(path.dirname(gitPath), '../bin/bash.exe') : '/bin/bash')

for (const [os, target] of [['macOS', 'darwin/amd64'], ['macOS', 'darwin/arm64'], ['Windows', 'windows/amd64']]) {
  test(`原生 GUI 构建参数适用于 ${target} 和旧版 Bash 严格模式`, () => {
    const result = spawnSync(bash, ['--noprofile', '--norc', '-c',
      `wails() { printf '%s\\n' "$@"; }\n${script.replaceAll('${{ matrix.target }}', target)}`], {
      encoding: 'utf8',
      env: { ...process.env, RUNNER_OS: os, BASH_COMPAT: '43' },
    })
    assert.ifError(result.error)
    assert.equal(result.status, 0, result.stderr)
    assert.deepEqual(result.stdout.trim().split(/\r?\n/), [
      'build', '-clean', '-platform', target, '-trimpath', '-m', '-nosyncgomod',
      ...(os === 'Windows' ? ['-webview2', 'embed'] : []),
    ])
  })
}
