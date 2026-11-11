/**
 * Theme selection business module: loads the catalog and the site's current
 * selection (with its strong ETag), edits the choice and the configuration
 * JSON, and saves through If-Match. 412 offers a conflict sync like the
 * settings editor.
 */
import { ApiError } from '../api/client';
import {
	getSite,
	getSiteTheme,
	listThemes,
	putSiteTheme,
	type Site,
	type Theme
} from '../api/management';
import { apiErrorKey } from '../i18n/errors';

export type ThemePhase =
	| { kind: 'loading' }
	| { kind: 'ready' }
	| { kind: 'saving' }
	| { kind: 'saved' }
	| { kind: 'error'; key: string }
	| { kind: 'conflict'; synced: boolean };

export interface ThemeSelectionState {
	site: Site | null;
	themes: Theme[];
	/** Catalog key of the edited choice: "themeId@version", or null. */
	selected: string | null;
	/** Whether any selection exists server-side. */
	hasSelection: boolean;
	configText: string;
	phase: ThemePhase;
}

export class ThemeSelection {
	private state: ThemeSelectionState = {
		site: null,
		themes: [],
		selected: null,
		hasSelection: false,
		configText: '{}',
		phase: { kind: 'loading' }
	};
	private etag = '';
	private listeners = new Set<() => void>();
	private loadToken = 0;

	constructor(private options: { onUnauthenticated: () => Promise<void> }) {}

	subscribe = (listener: () => void): (() => void) => {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	};

	getSnapshot = (): ThemeSelectionState => this.state;

	private set(patch: Partial<ThemeSelectionState>): void {
		this.state = { ...this.state, ...patch };
		for (const listener of this.listeners) listener();
	}

	editable(): boolean {
		return (
			this.state.site !== null &&
			(this.state.site.role === 'owner' || this.state.site.role === 'editor')
		);
	}

	dirty(): boolean {
		if (!this.state.hasSelection && this.state.selected === null) return this.state.configText.trim() !== '{}';
		return true; // any explicit save is meaningful; the server validates
	}

	async load(siteId: string): Promise<void> {
		const token = ++this.loadToken;
		this.set({ phase: { kind: 'loading' } });
		try {
			const [site, themes, selection] = await Promise.all([
				getSite(siteId),
				listThemes(),
				getSiteTheme(siteId)
			]);
			if (token !== this.loadToken) return;
			this.etag = selection.etag;
			this.set({
				site,
				themes,
				hasSelection: selection.selection.selected,
				selected: selection.selection.selected
					? `${selection.selection.theme?.themeId ?? ''}@${selection.selection.theme?.version ?? ''}`
					: null,
				configText: selection.selection.selected
					? JSON.stringify(selection.selection.themeConfig ?? {}, null, 2)
					: '{}',
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

	select(key: string | null): void {
		this.set({ selected: key });
	}

	setConfig(text: string): void {
		this.set({ configText: text });
	}

	async save(): Promise<void> {
		if (!this.editable() || this.state.phase.kind === 'saving') return;
		let config: unknown;
		try {
			config = JSON.parse(this.state.configText || '{}');
		} catch {
			this.set({ phase: { kind: 'error', key: 'error.validation' } });
			return;
		}
		const [themeId, version] = (this.state.selected ?? '@').split('@');
		if (!themeId || !version) {
			this.set({ phase: { kind: 'error', key: 'error.noThemeSelected' } });
			return;
		}
		this.set({ phase: { kind: 'saving' } });
		try {
			const { selection, etag } = await putSiteTheme(
				this.state.site!.siteId,
				{ themeId, version, themeConfig: config as Record<string, never> },
				this.etag
			);
			this.etag = etag;
			this.set({
				hasSelection: selection.selected,
				selected: selection.selected
					? `${selection.theme?.themeId ?? ''}@${selection.theme?.version ?? ''}`
					: null,
				configText: selection.selected
					? JSON.stringify(selection.themeConfig ?? {}, null, 2)
					: '{}',
				phase: { kind: 'saved' }
			});
		} catch (err) {
			if (err instanceof ApiError && err.status === 412) {
				this.set({ phase: { kind: 'conflict', synced: false } });
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

	async syncAfterConflict(siteId: string): Promise<void> {
		try {
			const { selection, etag } = await getSiteTheme(siteId);
			this.etag = etag;
			this.set({
				hasSelection: selection.selected,
				selected: selection.selected
					? `${selection.theme?.themeId ?? ''}@${selection.theme?.version ?? ''}`
					: null,
				configText: selection.selected
					? JSON.stringify(selection.themeConfig ?? {}, null, 2)
					: '{}',
				phase: { kind: 'conflict', synced: true }
			});
		} catch {
			this.set({ phase: { kind: 'error', key: 'error.generic' } });
		}
	}
}
