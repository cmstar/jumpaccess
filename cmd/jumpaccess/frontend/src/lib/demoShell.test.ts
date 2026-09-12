import { expect, test } from 'vitest'
import { DemoShell } from './demoShell'

test('模拟 Shell 支持分段输入、退格、中断与切换目录', () => {
  const shell = new DemoShell('deploy', 'sample-host')
  shell.write('pwX\x7fd')
  expect(shell.write('\r')).toContain('/home/deploy')
  shell.write('cd /srv/webapp\r')
  expect(shell.write('pwd\r')).toContain('/srv/webapp')
  shell.write('unused\x03')
  expect(shell.write('whoami\r')).toContain('\r\ndeploy\r\n')
})

test('任意系统命令仅提示不支持，终端控制序列不成为命令', () => {
  const shell = new DemoShell('deploy', 'sample-host')
  expect(shell.write('rm -rf /\r')).toContain('Demo: unsupported command')
  shell.write('\x1b[A')
  expect(shell.write('pwd\r')).toContain('\r\n/home/deploy\r\n')
})
