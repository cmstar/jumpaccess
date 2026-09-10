import { createContext, useContext, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { Backend } from '../lib/backend'
import { backgroundLayout, defaultTerminalBackground, type TerminalBackground } from '../model/terminalBackground'

interface LoadedImage { path: string; url: string; width: number; height: number }
interface BackgroundState { settings: TerminalBackground; image?: LoadedImage; error?: string; loading?: boolean }
const BackgroundContext = createContext<BackgroundState>({ settings: defaultTerminalBackground })
export const useTerminalBackground = () => useContext(BackgroundContext)

export function TerminalBackgroundAppearance({ settings, children }: { settings: TerminalBackground; children: ReactNode }) {
  const current = useTerminalBackground()
  return <BackgroundContext.Provider value={{ ...current, settings }}>{children}</BackgroundContext.Provider>
}

// 一个应用实例只读取、验证一次当前图片；调整透明度、尺寸或切换 Tab 不重新读取。
export function TerminalBackgroundProvider({ backend, settings, children }: { backend: Backend; settings: TerminalBackground; children: ReactNode }) {
  const [loaded, setLoaded] = useState<{ path: string; image?: LoadedImage; error?: string }>({ path: '' })
  const path = settings.enabled ? settings.filePath : ''
  useEffect(() => {
    if (!path) { setLoaded({ path: '' }); return }
    let cancelled = false
    const image = new Image()
    setLoaded({ path })
    void backend.readTerminalBackground(path).then(url => {
      if (cancelled) return
      image.onload = () => {
        if (cancelled) return
        if (!image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 40_000_000) {
          setLoaded({ path, error: '图片尺寸无效或超过 4000 万像素，已使用终端纯色背景。' })
        } else setLoaded({ path, image: { path, url, width: image.naturalWidth, height: image.naturalHeight } })
      }
      image.onerror = () => { if (!cancelled) setLoaded({ path, error: '无法解码图片，已使用终端纯色背景。' }) }
      image.src = url
    }).catch(reason => { if (!cancelled) setLoaded({ path, error: `${reason instanceof Error ? reason.message : String(reason)}；已使用终端纯色背景。` }) })
    return () => { cancelled = true; image.onload = null; image.onerror = null; image.src = '' }
  }, [backend, path])
  const current = path && loaded.path === path ? loaded : undefined
  return <BackgroundContext.Provider value={{ settings, image: current?.image, error: current?.error, loading: !!path && !current?.image && !current?.error }}>{children}</BackgroundContext.Provider>
}

export function TerminalBackgroundSurface({ children, className, style }: { children: ReactNode; className: string; style?: CSSProperties }) {
  const { settings, image } = useTerminalBackground()
  const layer = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })
  const tiled = !!image && settings.fitMode === 'tile'
  useEffect(() => {
    const element = layer.current
    if (!element || !tiled) return
    const observer = new ResizeObserver(() => setSize({ width: element.clientWidth, height: element.clientHeight }))
    setSize({ width: element.clientWidth, height: element.clientHeight })
    observer.observe(element)
    return () => observer.disconnect()
  }, [tiled])
  const layout = image && tiled ? backgroundLayout(image.width, image.height, size.width, size.height, settings.tileFitLongEdge, settings.tileOnlyWholeTiles) : undefined
  const imageStyle: CSSProperties | undefined = image ? {
    backgroundImage: `url(${JSON.stringify(image.url)})`,
    opacity: 1 - settings.transparencyPercent / 100,
    backgroundRepeat: tiled ? 'repeat' : 'no-repeat',
    backgroundSize: tiled ? (layout ? `${layout.tileWidth}px ${layout.tileHeight}px` : 'auto') : settings.fitMode === 'stretch' ? '100% 100%' : settings.fitMode,
    backgroundPosition: tiled ? '0 0' : settings.fitMode === 'cover' ? `${settings.positionXPercent}% ${settings.positionYPercent}%` : 'center',
    width: layout?.width, height: layout?.height,
  } : undefined
  return <div className={`${className} terminal-background-surface`} style={style} data-background-visible={!!image}>
    <div className="terminal-background-layer" aria-hidden="true" ref={layer}>{image ? <div className="terminal-background-image" style={imageStyle} /> : null}</div>
    {children}
  </div>
}
