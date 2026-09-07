import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({ plugins: [vue()], base: './', build: { rollupOptions: { external: ['/wails/runtime.js'] } }, server: { strictPort: true, port: 5173 } })
