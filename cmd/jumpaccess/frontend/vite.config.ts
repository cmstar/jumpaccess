import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    fs: {
      allow: ['..', '../../../internal/guiconfig/terminal-schemes.json'],
    },
    port: 3001,
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    // 仅在布局回归用例中显式载入样式，不影响其他组件测试。
    css: { include: [/App\.css\?inline$/] },
  },
})
