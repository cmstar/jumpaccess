import assert from 'node:assert/strict'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { setWailsVersion } from './set-wails-version.mjs'

test('setWailsVersion 只更新临时配置中的产品版本', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'jumpaccess-wails-version-'))
  const configPath = path.join(directory, 'wails.json')
  await writeFile(configPath, JSON.stringify({
    name: 'JumpAccess',
    info: { productName: 'JumpAccess', productVersion: '0.1.0' },
  }))

  await setWailsVersion(configPath, '1.2.3')

  const config = JSON.parse(await readFile(configPath, 'utf8'))
  assert.equal(config.info.productVersion, '1.2.3')
  assert.equal(config.info.productName, 'JumpAccess')
})

test('setWailsVersion 拒绝不适合平台元数据的版本号', async () => {
  await assert.rejects(
    setWailsVersion('unused.json', '1.2.3-beta.1'),
    /必须是 X\.Y\.Z/,
  )
})
