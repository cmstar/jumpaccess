import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { chromium } from 'playwright'
import { createServer } from 'vite'

// 显式调用才生成截图；不被 build、test、CI 或发布流程调用。
const checkOnly = process.argv.includes('--check')
if (process.argv.slice(2).some(arg => arg !== '--check')) throw new Error('仅支持 --check（检查全部场景但不截图）；生成截图时不需要参数。')
const terminalFont = process.platform === 'win32' ? 'Consolas' : process.platform === 'darwin' ? 'Menlo' : 'monospace'
const frontend = fileURLToPath(new URL('../', import.meta.url))
const destination = path.resolve(frontend, '../../../docs/screenshots')
const server = await createServer({ root: frontend, mode: 'demo', server: { host: '127.0.0.1', port: 0, open: false } })
let browser
const images = []
const errors = []

try {
  await server.listen()
  const origin = server.resolvedUrls.local[0]
  browser = await chromium.launch({ headless: true })
  // 为交互演示栏额外留出 40px，导出的应用区域仍为 1440×960。
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', colorScheme: 'light', reducedMotion: 'reduce' })
  // 演示截图只允许访问本次启动的本地服务。
  await context.route('**/*', route => {
    const url = new URL(route.request().url())
    if (url.origin === new URL(origin).origin || ['data:', 'blob:'].includes(url.protocol)) return route.continue()
    errors.push(`演示尝试访问外部地址：${url.origin}`)
    return route.abort()
  })
  const page = await context.newPage()
  page.setDefaultTimeout(15_000)
  page.on('pageerror', error => errors.push(error.message))
  await page.clock.install({ time: new Date('2026-09-12T02:24:00Z') })
  let clockPaused = false

  async function start(scenario = 'ready') {
    await page.clock.setSystemTime(new Date('2026-09-12T02:24:00Z'))
    await page.goto(`${origin}?scenario=${scenario}`)
    await page.getByRole('complementary', { name: '演示控制' }).waitFor()
    await page.locator('.app-shell').waitFor()
    await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; scroll-behavior: auto !important; }' })
    if (scenario !== 'empty') await page.getByRole('button', { name: '使用 web 连接', exact: true }).waitFor()
  }

  async function capture(name) {
    if (name.startsWith('assets-')) {
      const wrapped = await page.locator('.asset-table-card .inline-alias-item').evaluateAll(items => items.some(item => {
        const label = item.querySelector('.inline-alias-name').getBoundingClientRect()
        const actions = item.querySelector('.inline-alias-actions').getBoundingClientRect()
        return Math.abs((label.top + label.bottom) / 2 - (actions.top + actions.bottom) / 2) > 3
      }))
      if (wrapped) throw new Error('演示资产的 Alias 名称和操作按钮不能换行。')
    }
    await page.evaluate(async () => {
      await document.fonts.ready
      await Promise.all([...document.images].map(image => image.decode().catch(() => {})))
    })
    if (clockPaused) await page.clock.runFor(32)
    else await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await page.mouse.move(0, 0)
    const application = page.locator('.app-shell')
    const bounds = await application.boundingBox()
    const toolbarBounds = await page.locator('.demo-toolbar').boundingBox()
    if (!bounds || bounds.width !== 1440 || bounds.height !== 960 || !toolbarBounds || bounds.y < toolbarBounds.y + toolbarBounds.height) throw new Error('截图区域必须为不含演示操作栏的 1440×960 应用界面。')
    if (!checkOnly) images.push({ name, data: await application.screenshot({ animations: 'disabled', caret: 'hide' }) })
    console.log(`${checkOnly ? '已检查' : '已截取'} ${name}`)
  }

  await start()
  await page.getByRole('row').filter({ hasText: 'prod-web-01' }).locator('td').first().click()
  await page.getByRole('complementary', { name: '资产详情' }).getByRole('button', { name: '连接 prod-web-01', exact: true }).waitFor()
  await capture('assets-light.png')

  await page.getByRole('button', { name: '打开设置', exact: true }).click()
  await page.getByRole('button', { name: '深色', exact: true }).click()
  await page.waitForFunction(() => document.documentElement.classList.contains('dark') || document.body.classList.contains('dark'))
  await page.getByRole('button', { name: '打开资产', exact: true }).click()
  await capture('assets-dark.png')

  await start()
  await page.getByRole('button', { name: '新建连接', exact: true }).click()
  const quickDialog = page.getByRole('dialog', { name: '快速连接', exact: true })
  await quickDialog.getByRole('combobox', { name: '搜索资产或别名', exact: true }).fill('prod')
  await quickDialog.getByText('（deploy）', { exact: true }).waitFor()
  await quickDialog.getByText('（dba）', { exact: true }).waitFor()
  const quickTargets = await quickDialog.locator('.quick-result-text strong').allTextContents()
  const expectedQuickTargets = ['web', 'ops', 'web-any', 'prod-db', 'prod-web-01']
  if (JSON.stringify(quickTargets) !== JSON.stringify(expectedQuickTargets)) throw new Error('快速连接截图应展示 prod 搜索得到的多个别名和资产结果。')
  for (const target of expectedQuickTargets) {
    for (const protocol of ['SSH', 'SFTP']) {
      if (!await quickDialog.getByRole('button', { name: `${target}：连接 ${protocol}`, exact: true }).isEnabled()) throw new Error('快速连接截图中的账号与协议必须加载完成，并展示可用的 SSH/SFTP 入口。')
    }
  }
  if (await quickDialog.getByRole('row', { selected: true }).count() !== 1) throw new Error('快速连接截图必须保留一个选中结果。')
  await capture('quick-connect.png')

  await start()
  await page.getByRole('button', { name: /^打开 Profile，/ }).click()
  await page.getByRole('heading', { name: 'Profile', exact: true }).waitFor()
  const overviewCards = page.getByRole('article')
  await overviewCards.first().getByRole('heading', { name: 'office', exact: true }).waitFor()
  if (await overviewCards.count() !== 2 || !(await overviewCards.first().textContent()).includes('https://jump.example.com')) throw new Error('Profile 总览应延续教程中的 office 站点，并保留第二个演示 Profile。')
  await capture('profiles.png')

  await start()
  await page.getByRole('button', { name: '打开设置', exact: true }).click()
  await page.getByRole('navigation', { name: '设置导航' }).getByRole('button', { name: '终端样式', exact: true }).click()
  await page.getByRole('combobox', { name: '字体', exact: true }).fill(terminalFont)
  await page.getByRole('combobox', { name: '字体', exact: true }).press('Enter')
  await page.getByLabel('字号', { exact: true }).selectOption('14')
  await page.getByLabel('行高', { exact: true }).selectOption('1.2')
  await page.waitForFunction(font => {
    return document.querySelector('#terminal-font-family')?.value === font
      && document.querySelector('#terminal-font-size')?.value === '14'
      && document.querySelector('#terminal-line-height')?.value === '1.2'
  }, terminalFont)
  await page.getByRole('button', { name: '关闭 设置 Tab', exact: true }).click()
  await page.getByRole('button', { name: '打开资产', exact: true }).click()
  await page.getByRole('button', { name: '使用 web 连接', exact: true }).click()
  await page.locator('.terminal-host .xterm-rows').filter({ hasText: 'Type help' }).waitFor()
  await page.locator('.terminal-host .xterm-helper-textarea').focus()
  for (const command of ['whoami', 'cd /srv/webapp', 'ls -la', 'cat app.yaml', 'df -h']) {
    await page.keyboard.type(command)
    await page.keyboard.press('Enter')
  }
  await page.locator('.terminal-host .xterm-rows').filter({ hasText: '/dev/vdb1' }).waitFor()
  await page.waitForFunction(font => {
    const rows = document.querySelector('.terminal-host .xterm-rows')
    if (!rows) return false
    const style = getComputedStyle(rows)
    return style.fontSize === '14px' && style.fontFamily.includes(font)
  }, terminalFont)
  // 检查实际使用的本机字体，防止指定名称后悄悄回退到通用等宽字体。
  if (terminalFont !== 'monospace') {
    await page.evaluate(() => document.fonts.ready)
    const cdp = await context.newCDPSession(page)
    try {
      await cdp.send('DOM.enable')
      await cdp.send('CSS.enable')
      const { root } = await cdp.send('DOM.getDocument')
      const { nodeId } = await cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector: '.terminal-host .xterm-rows > div:first-child' })
      const { fonts } = await cdp.send('CSS.getPlatformFontsForNode', { nodeId })
      if (!fonts.some(font => font.familyName === terminalFont && font.glyphCount > 0)) throw new Error(`SSH 截图未实际使用 ${terminalFont}，请确认本机已安装该系统字体。`)
    } finally { await cdp.detach() }
  }
  await capture('ssh-terminal.png')

  await page.getByRole('button', { name: '从 SSH 连接 SFTP', exact: true }).click()
  await page.getByRole('button', { name: '打开 app.yaml', exact: true }).waitFor()
  // 用真实界面创建双向传输，通过浏览器时钟停在中途，避免截图碰运气。
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  clockPaused = true
  await page.getByRole('button', { name: '上传文件', exact: true }).click()
  await page.clock.runFor(450)
  await page.getByRole('checkbox', { name: '选择 app.yaml', exact: true }).check()
  await page.getByRole('button', { name: '下载', exact: true }).click()
  await page.clock.runFor(450)
  const runningTransfers = page.locator('.sftp-transfer.running')
  await runningTransfers.filter({ hasText: 'release.zip' }).waitFor()
  await runningTransfers.filter({ hasText: 'app.yaml' }).waitFor()
  const progresses = await runningTransfers.locator('progress').evaluateAll(elements => elements.map(element => element.value / element.max))
  if (progresses.length < 2 || progresses.some(value => value <= 0 || value >= 1)) throw new Error('SFTP 截图必须展示正在执行且已有进度的上传和下载任务。')
  await capture('sftp-files.png')
  await page.clock.runFor(2000)
  await page.clock.resume()
  clockPaused = false
  await page.getByRole('button', { name: 'Home 目录', exact: true }).click()
  await page.getByRole('button', { name: '打开 notes.txt', exact: true }).waitFor()
  await page.getByRole('button', { name: '上传文件', exact: true }).click()
  await page.getByRole('dialog', { name: '文件已存在', exact: true }).waitFor()
  await page.locator('.sftp-transfer.completed').filter({ hasText: 'release.zip' }).last().waitFor()
  await capture('sftp-conflict.png')
  await page.getByRole('button', { name: '保留两者', exact: true }).click()
  await page.getByRole('button', { name: '打开 notes (1).txt', exact: true }).waitFor()

  await start()
  await page.getByRole('button', { name: '打开设置', exact: true }).click()
  await page.getByRole('navigation', { name: '设置导航' }).getByRole('button', { name: '终端样式', exact: true }).click()
  await page.getByRole('region', { name: '终端预览', exact: true }).locator('.xterm-rows').filter({ hasText: 'jumpaccess' }).waitFor()
  await capture('terminal-settings.png')

  // 连续操作同一个 Profile，不在步骤间重置或直接注入已认证状态。
  const profileName = 'office'
  const siteURL = 'https://jump.example.com'
  const callbackURL = 'jms://auth/callback?code=demo-code&state=demo-state'
  await start('empty')
  await page.getByRole('heading', { name: '尚未创建 Profile', exact: true }).waitFor()
  await capture('profile-01-empty.png')

  await page.getByRole('button', { name: '添加 Profile', exact: true }).first().click()
  const createDialog = page.getByRole('dialog', { name: '添加 Profile', exact: true })
  await createDialog.waitFor()
  const addAndLogin = createDialog.getByRole('button', { name: '添加并登录', exact: true })
  if (await addAndLogin.isEnabled()) throw new Error('空白的新建 Profile 表单不应允许提交。')
  await capture('profile-02-create-empty.png')

  await createDialog.getByLabel('名称', { exact: true }).fill(profileName)
  await createDialog.getByLabel('JumpServer URL', { exact: true }).fill(siteURL)
  if (!await addAndLogin.isEnabled()) throw new Error('填写 Profile 名称和地址后应允许添加并登录。')
  await capture('profile-03-create-filled.png')
  await addAndLogin.click()

  const loginDialog = page.getByRole('dialog', { name: '完成浏览器登录', exact: true })
  await loginDialog.waitFor()
  if (!(await loginDialog.textContent()).includes(profileName)) throw new Error('授权弹窗中的 Profile 与新建的名称不一致。')
  const finishLogin = loginDialog.getByRole('button', { name: '完成登录', exact: true })
  if (await finishLogin.isEnabled()) throw new Error('空白的回调表单不应允许完成登录。')
  await capture('profile-04-callback-empty.png')

  await loginDialog.getByLabel('回调链接或确认页 URL', { exact: true }).fill(callbackURL)
  if (!await finishLogin.isEnabled()) throw new Error('填写演示回调链接后应允许完成登录。')
  await capture('profile-05-callback-filled.png')
  await finishLogin.click()
  await loginDialog.waitFor({ state: 'hidden' })
  const profileCard = page.getByRole('article').filter({ has: page.getByRole('heading', { name: profileName, exact: true }) })
  await profileCard.getByText('已认证', { exact: true }).waitFor()
  if (await page.getByRole('article').count() !== 1 || !(await profileCard.textContent()).includes(siteURL)) throw new Error('授权完成后应只展示本次创建的 Profile 和站点地址。')
  if (context.pages().length !== 1) throw new Error('演示授权不应打开外部浏览器页面。')
  await capture('profile-06-authorized.png')

  if (errors.length) throw new Error(errors.join('\n'))
  // 所有场景成功后才写入目标目录，不删除人工保存的其他图片。
  if (!checkOnly) {
    await mkdir(destination, { recursive: true })
    for (const { name, data } of images) await writeFile(path.join(destination, name), data)
    console.log(`已生成 ${images.length} 张截图：${destination}`)
  }
} finally {
  await browser?.close()
  await server.close()
}
