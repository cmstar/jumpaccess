import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

export async function setWailsVersion(configPath, version) {
  if (!/^\d+\.\d+\.\d+$/u.test(version)) {
    throw new Error('Wails 产品版本必须是 X.Y.Z 格式')
  }

  const config = JSON.parse(await readFile(configPath, 'utf8'))
  if (!config.info || typeof config.info !== 'object') {
    throw new Error(`${configPath} 缺少 info 配置`)
  }
  config.info.productVersion = version
  await writeFile(configPath, `${JSON.stringify(config, null, 2)}\n`, 'utf8')
}

async function main() {
  const [version, configPath = 'cmd/jumpaccess/wails.json'] = process.argv.slice(2)
  if (!version) {
    throw new Error('用法：node scripts/set-wails-version.mjs <X.Y.Z> [wails.json]')
  }
  await setWailsVersion(configPath, version)
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : ''
if (import.meta.url === entryPoint) {
  main().catch((error) => {
    console.error(error.message)
    process.exitCode = 1
  })
}
