/**
 * SiteList: framework-agnostic external store for the caller's site list.
 * The app shell resolves the current site from it, and the sites page renders
 * and refreshes it; both share one cached request per load generation so the
 * sidebar never flashes empty when navigating between site pages.
 */
import { listSites, type Site } from '../api/management';
import { currentAuthGeneration } from '../api/client';

export type SiteListStatus = 'idle' | 'loading' | 'ready' | 'error';

interface SiteListState {
	status: SiteListStatus;
	sites: Site[];
}

const empty: SiteListState = { status: 'idle', sites: [] };
let state: SiteListState = empty;
let generation = -1;
let requestRevision = 0;
const listeners = new Set<() => void>();

function emit(): void {
	for (const listener of listeners) listener();
}

export function subscribeSiteList(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function siteListSnapshot(): SiteListState {
	return generation === currentAuthGeneration() ? state : empty;
}

let inflight: Promise<void> | null = null;
let pendingEdits: Map<string, Site> | null = null;

function adoptGeneration(next: number) {
	if (generation === next) return;
	generation = next;
	state = empty;
	inflight = null;
	pendingEdits = null;
}

/** Loads (or reloads) the site list once per call generation. */
export async function loadSiteList(force = false): Promise<void> {
	const ownGeneration = currentAuthGeneration();
	adoptGeneration(ownGeneration);
	if (state.status === 'loading') return inflight ?? Promise.resolve();
	if (state.status === 'ready' && !force) return Promise.resolve();
	const edits = new Map<string, Site>();
	pendingEdits = edits;
	const revision = ++requestRevision;
	const current = () =>
		requestRevision === revision &&
		generation === ownGeneration &&
		currentAuthGeneration() === ownGeneration;
	state = { ...state, status: 'loading' };
	inflight = listSites()
		.then((sites) => {
			if (!current()) return;
			state = {
				status: 'ready',
				sites: [...edits.values(), ...sites.filter((site) => !edits.has(site.siteId))]
			};
		})
		.catch(() => {
			if (!current()) return;
			state = { status: 'error', sites: [] };
		})
		.finally(() => {
			if (!current()) return;
			inflight = null;
			pendingEdits = null;
			emit();
		});
	emit();
	return inflight;
}

/** Locally upserts a site (after creation) without a full reload. */
export function upsertSite(site: Site, expectedGeneration: number): void {
	if (expectedGeneration !== currentAuthGeneration()) return;
	adoptGeneration(expectedGeneration);
	pendingEdits?.set(site.siteId, site);
	const next = state.sites.filter((s) => s.siteId !== site.siteId);
	next.unshift(site);
	state = { status: 'ready', sites: next };
	emit();
}
