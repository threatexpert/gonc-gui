import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import {readFileSync} from 'node:fs'

const appVersion = readFileSync(new URL('../VERSION', import.meta.url), 'utf-8').trim()

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion)
  }
})
