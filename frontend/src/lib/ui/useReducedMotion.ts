import { useSyncExternalStore } from 'react';

// Preference helper from robot-platform's modules/login/ui/src/loginMotion.ts.
const query = '(prefers-reduced-motion: reduce)';

function subscribe(notify: () => void) {
	const media = window.matchMedia(query);
	media.addEventListener('change', notify);
	return () => media.removeEventListener('change', notify);
}

const read = () => window.matchMedia(query).matches;

/** Whether the system asks to reduce motion, kept current when it changes. */
export function useReducedMotion() {
	return useSyncExternalStore(subscribe, read, () => true);
}
