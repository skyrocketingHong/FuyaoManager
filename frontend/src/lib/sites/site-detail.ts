/**
 * Site detail business module: loads one member site and keeps the loading
 * state race-safe, so a slow response for a previously viewed site can never
 * overwrite the currently displayed one. Framework-agnostic store.
 */
import { getSite, type Site } from '../api/management';
import { apiErrorKey } from '../i18n/errors';

export interface SiteDetailState {
	loading: boolean;
	site: Site | null;
	errorKey: string | null;
}

export class SiteDetail {
	private state: SiteDetailState = { loading: true, site: null, errorKey: null };
	private loadToken = 0;
	private listeners = new Set<() => void>();

	subscribe = (listener: () => void): (() => void) => {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	};

	getSnapshot = (): SiteDetailState => this.state;

	private set(patch: Partial<SiteDetailState>): void {
		this.state = { ...this.state, ...patch };
		for (const listener of this.listeners) listener();
	}

	async load(siteId: string, unauthenticated: () => Promise<void>): Promise<void> {
		const token = ++this.loadToken;
		this.set({ loading: true, site: null, errorKey: null });
		try {
			const site = await getSite(siteId);
			if (token !== this.loadToken) return; // a newer load superseded this one
			this.set({ site, loading: false });
		} catch (err) {
			if (token !== this.loadToken) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await unauthenticated();
				return;
			}
			this.set({ errorKey: key ?? 'error.generic', loading: false });
		}
	}
}
