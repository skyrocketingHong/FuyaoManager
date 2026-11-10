/**
 * Site detail business module: loads one member site and keeps the loading
 * state race-safe, so a slow response for a previously viewed site can never
 * overwrite the currently displayed one.
 */
import { getSite, type Site } from '$lib/api/management';
import { apiErrorKey } from '$lib/i18n/errors';

export interface SiteDetailState {
	loading: boolean;
	site: Site | null;
	errorKey: string | null;
}

export function createSiteDetail() {
	const state = $state<SiteDetailState>({ loading: true, site: null, errorKey: null });
	let loadToken = 0;

	async function load(siteId: string, unauthenticated: () => Promise<void>): Promise<void> {
		const token = ++loadToken;
		state.loading = true;
		state.site = null;
		state.errorKey = null;
		try {
			const site = await getSite(siteId);
			if (token !== loadToken) return; // a newer load superseded this one
			state.site = site;
			state.loading = false;
		} catch (err) {
			if (token !== loadToken) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await unauthenticated();
				return;
			}
			state.errorKey = key ?? 'error.generic';
			state.loading = false;
		}
	}

	return { state, load };
}
