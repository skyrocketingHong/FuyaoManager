/**
 * Settings editing business module.
 *
 * Concurrency semantics:
 * - Only fields the user actually changed are submitted, so edits never
 *   clobber server-side changes to untouched fields.
 * - The baseline (last known server state) is separate from the form: a
 *   successful save adopts the server response as the new baseline while the
 *   form keeps anything typed during the request (those stay visibly
 *   unsaved), and a 412 offers a sync that refreshes baseline + ETag without
 *   discarding the user's edits. Fields whose edits now equal the server
 *   value converge to non-dirty automatically; genuinely diverging fields
 *   stay dirty and are re-sent on the user's explicit next save.
 * - Loads are token-guarded so out-of-order responses for another site can
 *   never land here.
 */
import { ApiError } from '$lib/api/client';
import {
	getSettings,
	getSite,
	updateSettings,
	type Site,
	type SiteSettings,
	type SiteSettingsPatch
} from '$lib/api/management';
import { apiErrorKey } from '$lib/i18n/errors';

export type SettingsField = 'name' | 'description' | 'defaultLocale';

export type SettingsPhase =
	| { kind: 'loading' }
	| { kind: 'ready' }
	| { kind: 'saving' }
	| { kind: 'saved'; revision: number }
	| { kind: 'error'; key: string }
	| { kind: 'conflict'; serverRevision: number | null; synced: boolean };

export interface SettingsEditorState {
	site: Site | null;
	phase: SettingsPhase;
	name: string;
	description: string;
	defaultLocale: 'zh-CN' | 'en-US';
}

export interface SettingsEditorOptions {
	/** Called when the API answers 401; the page redirects to login. */
	onUnauthenticated: () => Promise<void>;
}

export function createSettingsEditor(options: SettingsEditorOptions) {
	const state = $state<SettingsEditorState>({
		site: null,
		phase: { kind: 'loading' },
		name: '',
		description: '',
		defaultLocale: 'zh-CN'
	});

	// The baseline must be reactive ($state, not a plain closure variable):
	// UI derivations like the save-button state read it through dirtyFields,
	// so adopting a fresh server baseline (after a save or conflict sync)
	// must immediately recompute dirtiness. etag and the load token are never
	// rendered, so they stay plain.
	let baseline = $state<SiteSettings | null>(null);
	let etag = '';
	let loadToken = 0;

	function dirtyFields(): SettingsField[] {
		if (baseline === null) return [];
		const dirty: SettingsField[] = [];
		if (state.name !== baseline.name) dirty.push('name');
		if (state.description !== baseline.description) dirty.push('description');
		if (state.defaultLocale !== baseline.defaultLocale) dirty.push('defaultLocale');
		return dirty;
	}

	function editable(): boolean {
		return state.site !== null && (state.site.role === 'owner' || state.site.role === 'editor');
	}

	async function load(siteId: string): Promise<void> {
		const token = ++loadToken;
		state.site = null;
		baseline = null;
		state.phase = { kind: 'loading' };
		try {
			const site = await getSite(siteId);
			if (token !== loadToken) return;
			const { settings, etag: current } = await getSettings(siteId);
			if (token !== loadToken) return;
			state.site = site;
			baseline = settings;
			etag = current;
			state.name = settings.name;
			state.description = settings.description;
			state.defaultLocale = settings.defaultLocale;
			state.phase = { kind: 'ready' };
		} catch (err) {
			if (token !== loadToken) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await options.onUnauthenticated();
				return;
			}
			state.phase = { kind: 'error', key: key ?? 'error.generic' };
		}
	}

	async function save(): Promise<void> {
		if (!editable() || state.phase.kind === 'saving' || state.phase.kind === 'loading') return;
		const dirty = dirtyFields();
		if (dirty.length === 0 || state.site === null) return;

		// Snapshot the submitted fields at submit time; typing during the
		// request must not silently join this save nor be mistaken for saved.
		const patch: SiteSettingsPatch = {};
		if (dirty.includes('name')) patch.name = state.name;
		if (dirty.includes('description')) patch.description = state.description;
		if (dirty.includes('defaultLocale')) patch.defaultLocale = state.defaultLocale;
		const submitted = {
			name: state.name,
			description: state.description,
			defaultLocale: state.defaultLocale
		};

		state.phase = { kind: 'saving' };
		try {
			const result = await updateSettings(state.site.siteId, patch, etag);
			// Adopt the merged server state as the new baseline and ETag. For
			// each field the user has not touched since submitting, also adopt
			// the server's value into the form, so server-side normalization
			// (e.g. trimming a name) converges to clean instead of pinning a
			// stale pre-normalized text that would stay dirty forever. Fields
			// typed during the flight keep the user's newer input and remain
			// visibly unsaved.
			baseline = result.settings;
			etag = result.etag;
			if (state.name === submitted.name) state.name = result.settings.name;
			if (state.description === submitted.description) {
				state.description = result.settings.description;
			}
			if (state.defaultLocale === submitted.defaultLocale) {
				state.defaultLocale = result.settings.defaultLocale;
			}
			state.site = {
				...state.site,
				name: result.settings.name,
				description: result.settings.description,
				defaultLocale: result.settings.defaultLocale,
				settingsRevision: result.settings.revision
			};
			state.phase = { kind: 'saved', revision: result.settings.revision };
		} catch (err) {
			if (err instanceof ApiError && err.status === 412) {
				state.phase = { kind: 'conflict', serverRevision: null, synced: false };
				return;
			}
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await options.onUnauthenticated();
				return;
			}
			state.phase = { kind: 'error', key: key ?? 'error.generic' };
		}
	}

	// Conflict recovery: fetch the latest revision and ETag. Fields the user
	// has not edited since the last adoption adopt the new server values
	// (preventing stale values from ever being re-sent); genuinely edited
	// fields keep the user's value and stay dirty, re-sent only on the user's
	// explicit next save after they have seen the conflict notice.
	async function syncAfterConflict(): Promise<void> {
		if (state.site === null) return;
		try {
			const { settings, etag: current } = await getSettings(state.site.siteId);
			if (baseline !== null) {
				if (state.name === baseline.name) state.name = settings.name;
				if (state.description === baseline.description) state.description = settings.description;
				if (state.defaultLocale === baseline.defaultLocale)
					state.defaultLocale = settings.defaultLocale;
			}
			baseline = settings;
			etag = current;
			state.phase = { kind: 'conflict', serverRevision: settings.revision, synced: true };
		} catch {
			state.phase = { kind: 'error', key: 'error.generic' };
		}
	}

	return { state, dirtyFields, editable, load, save, syncAfterConflict };
}
