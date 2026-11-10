import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	// Vitest runs the SSR pipeline by default, which resolves Svelte to the
	// server runtime where $effect is a no-op. Force the browser build for
	// tests so reactive derivations behave exactly as in the app.
	resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
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
		// Node by default; the mounted-view suite opts into jsdom via its
		// per-file `@vitest-environment jsdom` pragma, which keeps the shared
		// SvelteKit plugin chain (runes compilation, $lib aliases) active for
		// every test.
		include: ['tests/**/*.test.ts'],
		environment: 'node'
	}
});
