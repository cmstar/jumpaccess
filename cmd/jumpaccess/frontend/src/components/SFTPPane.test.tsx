import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'
import { wailsBackend, type Backend, type SFTPEntry, type SFTPTransfer } from '../lib/backend'
import type { SFTPTab } from '../model/tabs'
import { SFTPPane } from './SFTPPane'

const tab: SFTPTab = { id: 'tab-1', kind: 'sftp', connectionStatus: 'active', sessionID: 'sftp-1', directory: '/home/deploy', descriptor: { profile: 'production', organization: 'org-1', assetID: 'asset-1', assetName: 'prod-web-01', account: 'account-1', target: 'asset-1', alias: '' } }
const entries: SFTPEntry[] = [
  { name: 'releases', path: '/home/deploy/releases', type: 'directory', size: 0, modifiedAt: '2026-09-01T01:00:00Z', permissions: 'drwxr-xr-x' },
  { name: 'README.md', path: '/home/deploy/README.md', type: 'file', size: 1024, modifiedAt: '2026-09-01T01:00:00Z', permissions: '-rw-r--r--' },
  { name: '.env', path: '/home/deploy/.env', type: 'file', size: 42, modifiedAt: '2026-09-01T01:00:00Z', permissions: '-rw-------' },
]
function backendFor(overrides: Partial<Backend> = {}): Backend {
  return { ...wailsBackend,
    readSFTPDirectory: vi.fn(async (_id, path) => ({ path: path === '.' ? '/home/deploy' : path, entries: path === '/home/deploy/releases' ? [] : entries })),
    homeSFTPDirectory: vi.fn(async () => ({ path: '/home/deploy', entries })),
    listSFTPTransfers: vi.fn(async () => []),
    onSFTPTransfer: vi.fn(() => () => undefined),
    makeSFTPDirectory: vi.fn(async () => undefined), renameSFTPEntry: vi.fn(async () => undefined), removeSFTPEntries: vi.fn(async () => undefined),
    chooseSFTPUploadFiles: vi.fn(async () => ['C:\\Downloads\\config.json']), chooseSFTPUploadDirectory: vi.fn(async () => 'C:\\Downloads\\site'), chooseSFTPDownloadDirectory: vi.fn(async () => 'C:\\Downloads'),
    startSFTPTransfer: vi.fn(async () => []), cancelSFTPTransfer: vi.fn(async () => undefined), retrySFTPTransfer: vi.fn(async () => transfer), resolveSFTPConflict: vi.fn(async () => undefined), clearCompletedSFTPTransfers: vi.fn(async () => undefined),
    ...overrides,
  }
}
function show(backend: Backend, currentTab = tab) { return render(<SFTPPane backend={backend} tab={currentTab} onReconnect={vi.fn()} onDisconnect={vi.fn()} />) }

test('浏览远程目录并切换隐藏文件，输入路径后进入服务器返回目录', async () => {
  const backend = backendFor()
  show(backend)
  expect(await screen.findByRole('button', { name: '打开 releases' })).toBeInTheDocument()
  expect(screen.queryByText('.env')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('checkbox', { name: '显示隐藏文件' }))
  expect(screen.getByText('.env')).toBeInTheDocument()
  await userEvent.dblClick(screen.getByRole('button', { name: '打开 releases' }))
  expect(await screen.findByText('此目录为空')).toBeInTheDocument()
  expect(screen.getByRole('textbox', { name: '远程路径' })).toHaveValue('/home/deploy/releases')
  await userEvent.clear(screen.getByRole('textbox', { name: '远程路径' }))
  await userEvent.type(screen.getByRole('textbox', { name: '远程路径' }), '/home/deploy{Enter}')
  expect(await screen.findByText('README.md')).toBeInTheDocument()
})

test('在当前目录新建文件夹，服务器失败保留输入并提示原因', async () => {
  const backend = backendFor({ makeSFTPDirectory: vi.fn().mockRejectedValueOnce(new Error('permission denied')).mockResolvedValue(undefined) })
  show(backend)
  await screen.findByText('README.md')
  await userEvent.click(screen.getByRole('button', { name: '新建文件夹' }))
  await userEvent.type(screen.getByRole('textbox', { name: '文件夹名称' }), 'logs')
  await userEvent.click(screen.getByRole('button', { name: '创建' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('permission denied')
  expect(screen.getByRole('textbox', { name: '文件夹名称' })).toHaveValue('logs')
  await userEvent.click(screen.getByRole('button', { name: '创建' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(backend.makeSFTPDirectory).toHaveBeenCalledWith('sftp-1', '/home/deploy/logs')
})

test('选中文件后重命名，批量删除必须确认且取消不删除', async () => {
  const backend = backendFor()
  show(backend)
  await screen.findByText('README.md')
  await userEvent.click(screen.getByRole('checkbox', { name: '选择 README.md' }))
  await userEvent.click(screen.getByRole('button', { name: '重命名' }))
  await userEvent.clear(screen.getByRole('textbox', { name: '新名称' }))
  await userEvent.type(screen.getByRole('textbox', { name: '新名称' }), 'NOTES.md')
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(backend.renameSFTPEntry).toHaveBeenCalledWith('sftp-1', '/home/deploy/README.md', 'NOTES.md'))
  await userEvent.click(screen.getByRole('checkbox', { name: '全选文件' }))
  await userEvent.click(screen.getByRole('button', { name: '删除' }))
  expect(screen.getByRole('dialog', { name: '删除文件' })).toHaveTextContent('2 个项目')
  await userEvent.click(screen.getByRole('button', { name: '取消' }))
  expect(backend.removeSFTPEntries).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: '删除' }))
  await userEvent.click(screen.getByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(backend.removeSFTPEntries).toHaveBeenCalledWith('sftp-1', ['/home/deploy/releases', '/home/deploy/README.md']))
})

const transfer: SFTPTransfer = { id: 'transfer-1', batchId: 'batch-1', sessionId: 'sftp-1', direction: 'upload', name: 'config.json', source: 'C:\\Downloads\\config.json', destination: '/home/deploy/config.json', status: 'running', transferred: 256, total: 1024, error: '' }

test('文件和目录通过系统选择器上传，多选远程项目下载到选定目录', async () => {
  const backend = backendFor({ startSFTPTransfer: vi.fn(async () => [transfer]) })
  show(backend)
  await screen.findByText('README.md')
  await userEvent.click(screen.getByRole('button', { name: '上传文件' }))
  expect(await screen.findByRole('region', { name: '传输队列' })).toHaveTextContent('config.json')
  expect(backend.startSFTPTransfer).toHaveBeenLastCalledWith({ sessionId: 'sftp-1', direction: 'upload', sources: ['C:\\Downloads\\config.json'], destination: '/home/deploy' })
  await userEvent.click(screen.getByRole('button', { name: '上传文件夹' }))
  expect(backend.startSFTPTransfer).toHaveBeenLastCalledWith({ sessionId: 'sftp-1', direction: 'upload', sources: ['C:\\Downloads\\site'], destination: '/home/deploy' })
  await userEvent.click(screen.getByRole('checkbox', { name: '全选文件' }))
  await userEvent.click(screen.getByRole('button', { name: '下载' }))
  expect(backend.startSFTPTransfer).toHaveBeenLastCalledWith({ sessionId: 'sftp-1', direction: 'download', sources: ['/home/deploy/releases', '/home/deploy/README.md'], destination: 'C:\\Downloads' })
})

test('传输事件更新队列，冲突可应用批次，失败保留原因并允许从头重试', async () => {
  let emit: (event: SFTPTransfer) => void = () => undefined
  const backend = backendFor({
    listSFTPTransfers: vi.fn(async () => [transfer]),
    onSFTPTransfer: vi.fn((handler) => { emit = handler; return () => undefined }),
    retrySFTPTransfer: vi.fn(async () => ({ ...transfer, status: 'queued' as const, transferred: 0 })),
  })
  show(backend)
  const queue = await screen.findByRole('region', { name: '传输队列' })
  await act(async () => emit({ ...transfer, status: 'conflict', conflict: { source: transfer.source, destination: transfer.destination } }))
  const dialog = screen.getByRole('dialog', { name: '文件已存在' })
  await userEvent.click(within(dialog).getByRole('checkbox', { name: '应用到本批次' }))
  await userEvent.click(within(dialog).getByRole('button', { name: '保留两者' }))
  expect(backend.resolveSFTPConflict).toHaveBeenCalledWith('transfer-1', 'keep-both', true)
  await act(async () => emit({ ...transfer, status: 'failed', error: 'disk full' }))
  expect(queue).toHaveTextContent('disk full')
  await userEvent.click(within(queue).getByRole('button', { name: '重试 config.json' }))
  expect(backend.retrySFTPTransfer).toHaveBeenCalledWith('transfer-1')
  expect(await within(queue).findByText('等待中')).toBeInTheDocument()
  await userEvent.click(within(queue).getByRole('button', { name: '取消 config.json' }))
  expect(backend.cancelSFTPTransfer).toHaveBeenCalledWith('transfer-1')
})

test('只有可见的 SFTP 面板接收系统文件拖入并上传到当前浏览目录', async () => {
  let dropped: ((_x: number, _y: number, paths: string[]) => void) | undefined
  const onDrop = vi.fn((handler) => { dropped = handler })
  const offDrop = vi.fn(() => { dropped = undefined })
  window.runtime = { EventsOnMultiple: vi.fn(() => () => undefined), OnFileDrop: onDrop, OnFileDropOff: offDrop }
  const backend = backendFor()
  const { rerender } = render(<SFTPPane backend={backend} tab={tab} active={false} onReconnect={vi.fn()} onDisconnect={vi.fn()} />)
  await screen.findByText('README.md')
  expect(onDrop).not.toHaveBeenCalled()
  rerender(<SFTPPane backend={backend} tab={tab} active onReconnect={vi.fn()} onDisconnect={vi.fn()} />)
  await userEvent.dblClick(screen.getByRole('button', { name: '打开 releases' }))
  await screen.findByText('此目录为空')
  await act(async () => dropped?.(50, 50, ['C:\\Downloads\\report.txt']))
  expect(backend.startSFTPTransfer).toHaveBeenCalledWith({ sessionId: 'sftp-1', direction: 'upload', sources: ['C:\\Downloads\\report.txt'], destination: '/home/deploy/releases' })
  rerender(<SFTPPane backend={backend} tab={tab} active={false} onReconnect={vi.fn()} onDisconnect={vi.fn()} />)
  expect(dropped).toBeUndefined()
  delete window.runtime
})

test('Home 与上级目录使用远程路径，刷新重新读取当前目录', async () => {
  const backend = backendFor({ homeSFTPDirectory: vi.fn(async () => ({ path: '/home/deploy', entries })) })
  show(backend)
  await screen.findByText('README.md')
  await userEvent.dblClick(screen.getByRole('button', { name: '打开 releases' }))
  await screen.findByText('此目录为空')
  await userEvent.click(screen.getByRole('button', { name: '上级目录' }))
  expect(await screen.findByText('README.md')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Home 目录' }))
  expect(backend.homeSFTPDirectory).toHaveBeenCalledWith('sftp-1')
  await userEvent.click(screen.getByRole('button', { name: '刷新目录' }))
  expect(backend.readSFTPDirectory).toHaveBeenLastCalledWith('sftp-1', '/home/deploy')
})

test('上传完成刷新当前目录，清理已完成任务会保留失败任务', async () => {
  let emit: (event: SFTPTransfer) => void = () => undefined
  const backend = backendFor({ listSFTPTransfers: vi.fn(async () => [transfer, { ...transfer, id: 'failed-2', name: 'failed.txt', status: 'failed' as const, error: 'permission denied' }]), onSFTPTransfer: vi.fn((handler) => { emit = handler; return () => undefined }) })
  show(backend)
  await screen.findByRole('region', { name: '传输队列' })
  const reads = vi.mocked(backend.readSFTPDirectory).mock.calls.length
  await act(async () => emit({ ...transfer, status: 'completed', transferred: 1024 }))
  await waitFor(() => expect(backend.readSFTPDirectory).toHaveBeenCalledTimes(reads + 1))
  await userEvent.click(screen.getByRole('button', { name: '清除已完成' }))
  const queue = screen.getByRole('region', { name: '传输队列' })
  expect(within(queue).queryByText('config.json')).not.toBeInTheDocument()
  expect(queue).toHaveTextContent('failed.txt')
  expect(backend.clearCompletedSFTPTransfers).toHaveBeenCalledWith('sftp-1')
})

test('同一个目录任务遇到后续文件及重试相同文件冲突时继续询问', async () => {
  let emit: (event: SFTPTransfer) => void = () => undefined
  const initial = { ...transfer, status: 'conflict' as const, conflict: { source: 'C:\\site\\a.txt', destination: '/home/deploy/site/a.txt' } }
  const backend = backendFor({ listSFTPTransfers: vi.fn(async () => [initial]), onSFTPTransfer: vi.fn((handler) => { emit = handler; return () => undefined }) })
  show(backend)
  await screen.findByRole('dialog', { name: '文件已存在' })
  await userEvent.click(screen.getByRole('button', { name: '覆盖' }))
  await act(async () => emit({ ...initial, conflict: { source: 'C:\\site\\b.txt', destination: '/home/deploy/site/b.txt' } }))
  expect(screen.getByRole('dialog', { name: '文件已存在' })).toHaveTextContent('/home/deploy/site/b.txt')
  await userEvent.click(screen.getByRole('button', { name: '跳过' }))
  await act(async () => emit({ ...transfer, status: 'running' }))
  await act(async () => emit(initial))
  expect(screen.getByRole('dialog', { name: '文件已存在' })).toHaveTextContent('/home/deploy/site/a.txt')
})

test('创建与重试的迟到返回值不能覆盖先到达的传输事件', async () => {
  let emit: (event: SFTPTransfer) => void = () => undefined
  const backend = backendFor({
    onSFTPTransfer: vi.fn((handler) => { emit = handler; return () => undefined }),
    startSFTPTransfer: vi.fn(async () => { emit({ ...transfer, status: 'completed', transferred: 1024 }); return [{ ...transfer, status: 'queued' as const, transferred: 0 }] }),
    retrySFTPTransfer: vi.fn(async () => { emit({ ...transfer, status: 'completed', transferred: 1024 }); return { ...transfer, status: 'queued' as const, transferred: 0 } }),
  })
  show(backend)
  await screen.findByText('README.md')
  await userEvent.click(screen.getByRole('button', { name: '上传文件' }))
  const queue = screen.getByRole('region', { name: '传输队列' })
  expect(within(queue).getByText('已完成', { exact: true })).toBeInTheDocument()
  await act(async () => emit({ ...transfer, status: 'failed', error: 'network error' }))
  await userEvent.click(within(queue).getByRole('button', { name: '重试 config.json' }))
  expect(within(queue).getByText('已完成', { exact: true })).toBeInTheDocument()
})

test('断开后仍接收该 Tab 的最终取消事件，并允许清理旧连接的已结束任务', async () => {
  let emit: ((event: SFTPTransfer) => void) | undefined
  const backend = backendFor({ listSFTPTransfers: vi.fn(async () => [transfer]), onSFTPTransfer: vi.fn((handler) => { emit = handler; return () => { emit = undefined } }) })
  const { rerender } = show(backend)
  await screen.findByRole('region', { name: '传输队列' })
  rerender(<SFTPPane backend={backend} tab={{ ...tab, sessionID: undefined, connectionStatus: 'disconnected' }} onReconnect={vi.fn()} onDisconnect={vi.fn()} />)
  await act(async () => emit?.({ ...transfer, status: 'cancelled' }))
  const queue = screen.getByRole('region', { name: '传输队列' })
  expect(within(queue).getByText('已取消', { exact: true })).toBeInTheDocument()
  expect(within(queue).queryByText('传输中', { exact: true })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '清除已完成' }))
  expect(screen.queryByRole('region', { name: '传输队列' })).not.toBeInTheDocument()
  expect(backend.clearCompletedSFTPTransfers).toHaveBeenCalledWith('sftp-1')
})
