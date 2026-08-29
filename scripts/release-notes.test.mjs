import assert from 'node:assert/strict'
import test from 'node:test'

import { parseCommit, renderReleaseNotes } from './release-notes.mjs'

test('parseCommit 识别 Conventional Commit 与破坏性变更', () => {
  assert.deepEqual(parseCommit({
    hash: '1234567890abcdef',
    subject: 'feat(cli)!: 调整列表输出格式',
    body: '',
  }), {
    breaking: true,
    description: '调整列表输出格式',
    hash: '1234567890abcdef',
    scope: 'cli',
    type: 'feat',
  })

  assert.equal(parseCommit({
    hash: 'abcdef1234567890',
    subject: 'fix(gui): 修复终端尺寸',
    body: 'BREAKING CHANGE: 配置格式已更新',
  }).breaking, true)
})

test('renderReleaseNotes 按类别输出提交和版本比较链接', () => {
  const notes = renderReleaseNotes({
    commits: [
      { hash: '1111111111111111', subject: 'feat(cli): 增加 Profile 更新命令', body: '' },
      { hash: '2222222222222222', subject: 'fix(gui): 修复终端尺寸', body: '' },
      { hash: '3333333333333333', subject: 'docs: 更新使用说明', body: '' },
    ],
    previousTag: 'v0.1.0',
    repository: 'cmstar/jumpaccess',
    tag: 'v0.2.0',
  })

  assert.match(notes, /## 新功能/)
  assert.match(notes, /\*\*cli\*\*：增加 Profile 更新命令/)
  assert.match(notes, /## 问题修复/)
  assert.match(notes, /## 文档更新/)
  assert.match(notes, /jumpctl-v0\.2\.0-windows-amd64\.zip/)
  assert.match(notes, /compare\/v0\.1\.0\.\.\.v0\.2\.0/)
})

test('renderReleaseNotes 为首个版本生成提交历史链接', () => {
  const notes = renderReleaseNotes({
    commits: [],
    previousTag: null,
    repository: 'cmstar/jumpaccess',
    tag: 'v0.1.0',
  })

  assert.match(notes, /本版本没有可列出的提交/)
  assert.match(notes, /commits\/v0\.1\.0/)
})
