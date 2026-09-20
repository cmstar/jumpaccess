import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const gitPath = process.platform === 'win32'
  ? spawnSync('where.exe', ['git'], { encoding: 'utf8' }).stdout.trim().split(/\r?\n/)[0] : null
const bash = process.env.JUMPACCESS_TEST_BASH ?? (gitPath
  ? path.resolve(path.dirname(gitPath), '../bin/bash.exe') : '/bin/bash')
const wrapper = fileURLToPath(new URL('./darwin-link.sh', import.meta.url))

function link(t, args, exitCode = 0) {
  const directory = mkdtempSync(path.join(tmpdir(), 'jumpaccess-link-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const clang = path.join(directory, 'clang')
  writeFileSync(clang, '#!/bin/bash\nprintf "%s\\n" "$@"\nexit "$TEST_CLANG_EXIT"\n')
  chmodSync(clang, 0o755)
  return spawnSync(bash, ['-c',
    'export PATH="$(cd "$1" && pwd):$PATH"; shift; bash "$@"',
    'test', directory, wrapper, ...args], {
    encoding: 'utf8', env: { ...process.env, TEST_CLANG_EXIT: String(exitCode), BASH_COMPAT: '43' },
  })
}

test('macOS 链接仅去重 libobjc，保留其他参数及严格警告', t => {
  const args = ['-arch', 'arm64', '-lobjc', 'object with spaces.o', '-Wl,-fatal_warnings',
    '-framework', 'Cocoa', '-lobjc', '-lother', '-lother', '-o', 'output with spaces']
  const result = link(t, args)
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trimEnd().split(/\r?\n/), args.filter((arg, i) => arg !== '-lobjc' || i === 2))
})

test('macOS 链接没有 libobjc 时原样传参并传播链接失败', t => {
  const args = ['-arch', 'x86_64', '-Wl,-fatal_warnings', '-o', 'output']
  const result = link(t, args, 17)
  assert.ifError(result.error)
  assert.equal(result.status, 17)
  assert.deepEqual(result.stdout.trimEnd().split(/\r?\n/), args)
})
