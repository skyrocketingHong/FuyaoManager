import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [react()],
	server: {
		// Same-origin development topology: the browser only talks to
		// http://localhost:5174, and /api is proxied to the loopback backend.
		// This keeps the WebAuthn origin (http://localhost:5174) and cookies
		// first-party; localhost pages calling 127.0.0.1 directly would be a
		// cross-site context where SameSite cookies are dropped.
		//
		// The port is pinned: the backend's allowed origins and the READMEs
		// reference 5174, and strictPort makes an occupied port fail loudly
		// instead of silently switching to another origin where passkeys and
		// cookies would break. The server binds loopback only.
		host: '127.0.0.1',
		port: 5174,
		strictPort: true,
		proxy: {
			'/api': {
				target: 'http://127.0.0.1:8787',
				changeOrigin: false
			}
		}
	},
	test: {
		// Node by default; mounted-view suites opt into jsdom via their
		// per-file `@vitest-environment jsdom` pragma.
		include: ['tests/**/*.test.tsx', 'tests/**/*.test.ts'],
		setupFiles: ['tests/setup.ts'],
		environment: 'node'
	}
});
