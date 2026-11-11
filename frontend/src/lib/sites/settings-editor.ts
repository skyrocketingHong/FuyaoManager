/**
 * Settings editing business module (framework-agnostic store).
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
import { ApiError } from '../api/client';
import {
	getSettings,
	getSite,
	updateSettings,
	type Site,
	type SiteSettings,
	type SiteSettingsPatch
} from '../api/management';
import { apiErrorKey } from '../i18n/errors';

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

export class SettingsEditor {
	private state: SettingsEditorState = {
		site: null,
		phase: { kind: 'loading' },
		name: '',
		description: '',
		defaultLocale: 'zh-CN'
	};
	private baseline: SiteSettings | null = null;
	private etag = '';
	private loadToken = 0;
	private listeners = new Set<() => void>();

	constructor(private options: SettingsEditorOptions) {}

	subscribe = (listener: () => void): (() => void) => {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	};

	getSnapshot = (): SettingsEditorState => this.state;

	private set(patch: Partial<SettingsEditorState>): void {
		this.state = { ...this.state, ...patch };
		for (const listener of this.listeners) listener();
	}

	dirtyFields(): SettingsField[] {
		if (this.baseline === null) return [];
		const dirty: SettingsField[] = [];
		if (this.state.name !== this.baseline.name) dirty.push('name');
		if (this.state.description !== this.baseline.description) dirty.push('description');
		if (this.state.defaultLocale !== this.baseline.defaultLocale) dirty.push('defaultLocale');
		return dirty;
	}

	editable(): boolean {
		return (
			this.state.site !== null &&
			(this.state.site.role === 'owner' || this.state.site.role === 'editor')
		);
	}

	async load(siteId: string): Promise<void> {
		const token = ++this.loadToken;
		this.baseline = null;
		this.set({ site: null, phase: { kind: 'loading' } });
		try {
			const site = await getSite(siteId);
			if (token !== this.loadToken) return;
			const { settings, etag: current } = await getSettings(siteId);
			if (token !== this.loadToken) return;
			this.baseline = settings;
			this.etag = current;
			this.set({
				site,
				name: settings.name,
				description: settings.description,
				defaultLocale: settings.defaultLocale,
				phase: { kind: 'ready' }
			});
		} catch (err) {
			if (token !== this.loadToken) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await this.options.onUnauthenticated();
				return;
			}
			this.set({ phase: { kind: 'error', key: key ?? 'error.generic' } });
		}
	}

	async save(): Promise<void> {
		if (!this.editable() || this.state.phase.kind === 'saving') return;
		const dirty = this.dirtyFields();
		if (dirty.length === 0 || this.state.site === null) return;

		// Snapshot the submitted fields at submit time; typing during the
		// request must not silently join this save nor be mistaken for saved.
		const patch: SiteSettingsPatch = {};
		if (dirty.includes('name')) patch.name = this.state.name;
		if (dirty.includes('description')) patch.description = this.state.description;
		if (dirty.includes('defaultLocale')) patch.defaultLocale = this.state.defaultLocale;
		const submitted = {
			name: this.state.name,
			description: this.state.description,
			defaultLocale: this.state.defaultLocale
		};

		this.set({ phase: { kind: 'saving' } });
		try {
			const result = await updateSettings(this.state.site.siteId, patch, this.etag);
			// Adopt the merged server state as the new baseline and ETag. For
			// each field the user has not touched since submitting, also adopt
			// the server's value, so server-side normalization (e.g. trimming
			// a name) converges to clean instead of pinning a stale
			// pre-normalized text that would stay dirty forever. Fields typed
			// during the flight keep the user's newer input and remain
			// visibly unsaved.
			this.baseline = result.settings;
			this.etag = result.etag;
			const next: Partial<SettingsEditorState> = { phase: { kind: 'saved', revision: result.settings.revision } };
			if (this.state.name === submitted.name) next.name = result.settings.name;
			if (this.state.description === submitted.description) {
				next.description = result.settings.description;
			}
			if (this.state.defaultLocale === submitted.defaultLocale) {
				next.defaultLocale = result.settings.defaultLocale;
			}
			if (this.state.site) {
				next.site = {
					...this.state.site,
					name: result.settings.name,
					description: result.settings.description,
					defaultLocale: result.settings.defaultLocale,
					settingsRevision: result.settings.revision
				};
			}
			this.set(next);
		} catch (err) {
			if (err instanceof ApiError && err.status === 412) {
				this.set({ phase: { kind: 'conflict', serverRevision: null, synced: false } });
				return;
			}
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				await this.options.onUnauthenticated();
				return;
			}
			this.set({ phase: { kind: 'error', key: key ?? 'error.generic' } });
		}
	}

	// Conflict recovery: fetch the latest revision and ETag. Fields the user
	// has not edited since the last adoption adopt the new server values
	// (preventing stale values from ever being re-sent); genuinely edited
	// fields keep the user's value and stay dirty, re-sent only on the user's
	// explicit next save after they have seen the conflict notice.
	async syncAfterConflict(): Promise<void> {
		if (this.state.site === null) return;
		try {
			const { settings, etag: current } = await getSettings(this.state.site.siteId);
			const next: Partial<SettingsEditorState> = {
				phase: { kind: 'conflict', serverRevision: settings.revision, synced: true }
			};
			if (this.baseline !== null) {
				if (this.state.name === this.baseline.name) next.name = settings.name;
				if (this.state.description === this.baseline.description) {
					next.description = settings.description;
				}
				if (this.state.defaultLocale === this.baseline.defaultLocale) {
					next.defaultLocale = settings.defaultLocale;
				}
			}
			this.baseline = settings;
			this.etag = current;
			this.set(next);
		} catch {
			this.set({ phase: { kind: 'error', key: 'error.generic' } });
		}
	}

	setField(field: SettingsField, value: string): void {
		if (field === 'defaultLocale') {
			this.set({ defaultLocale: value === 'en-US' ? 'en-US' : 'zh-CN' });
			return;
		}
		this.set({ [field]: value } as Partial<SettingsEditorState>);
	}
}
