import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// The admin console is a purely client-rendered SPA served behind the
		// development proxy or any static host; the backend API is same-origin.
		adapter: adapter({
			fallback: 'index.html'
		})
	}
};

export default config;
