import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import App from './App'
import { wailsBackend } from './lib/backend'

const container = document.getElementById('root')

const root = createRoot(container!)

async function start() {
  if (import.meta.env.MODE === 'demo') {
    if (window.go?.main?.desktopApp) throw new Error('演示入口仅允许在独立浏览器中运行。')
    const { DemoApp } = await import('./demo/DemoApp')
    root.render(<React.StrictMode><DemoApp /></React.StrictMode>)
    return
  }
  const backend = import.meta.env.DEV && import.meta.env.MODE !== 'test' && !window.go?.main?.desktopApp
    ? (await import('./lib/previewBackend')).previewBackend
    : wailsBackend
  root.render(
    <React.StrictMode>
      <App backend={backend} />
    </React.StrictMode>,
  )
}

void start()
