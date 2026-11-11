/**
 * SiteList: framework-agnostic external store for the caller's site list.
 * The app shell resolves the current site from it, and the sites page renders
 * and refreshes it; both share one cached request per load generation so the
 * sidebar never flashes empty when navigating between site pages.
 */
import { listSites, type Site } from '../api/management';

export type SiteListStatus = 'idle' | 'loading' | 'ready' | 'error';

interface SiteListState {
	status: SiteListStatus;
	sites: Site[];
}

let state: SiteListState = { status: 'idle', sites: [] };
const listeners = new Set<() => void>();

function emit(): void {
	for (const listener of listeners) listener();
}

export function subscribeSiteList(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function siteListSnapshot(): SiteListState {
	return state;
}

let inflight: Promise<void> | null = null;

/** Loads (or reloads) the site list once per call generation. */
export async function loadSiteList(force = false): Promise<void> {
	if (state.status === 'loading') return inflight ?? Promise.resolve();
	if (state.status === 'ready' && !force) return Promise.resolve();
	state = { ...state, status: 'loading' };
	emit();
	inflight = listSites()
		.then((sites) => {
			state = { status: 'ready', sites };
		})
		.catch(() => {
			state = { status: 'error', sites: [] };
		})
		.finally(() => {
			inflight = null;
			emit();
		});
	return inflight;
}

/** Locally upserts a site (after creation) without a full reload. */
export function upsertSite(site: Site): void {
	const next = state.sites.filter((s) => s.siteId !== site.siteId);
	next.unshift(site);
	state = { status: 'ready', sites: next };
	emit();
}
