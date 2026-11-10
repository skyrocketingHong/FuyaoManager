// The admin console is a purely client-rendered SPA; the backend is accessed
// same-origin through the dev proxy or a reverse proxy in deployment.
export const ssr = false;
export const prerender = false;
export const trailingSlash = 'never';
