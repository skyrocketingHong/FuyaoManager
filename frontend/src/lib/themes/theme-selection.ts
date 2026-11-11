/**
 * Theme selection business module: loads the catalog and the site's current
 * selection (with its strong ETag), edits the choice and the configuration
 * JSON, and saves through If-Match. 412 offers a conflict sync like the
 * settings editor.
 *
 * Draft semantics: the radio choice and the configuration textarea are an
 * editing draft, kept strictly separate from the saved baseline. A save
 * adopts the server response only for fields untouched since submitting;
 * typing during the flight and during a 412 sync stays in the form and is
 * never silently reset. The "current saved theme" is always the saved
 * baseline, never the unsaved radio value.
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
	/** Catalog key of the edited draft choice: "themeId@version", or null. */
	selected: string | null;
	/** The saved baseline choice ("themeId@version"), null when unselected. */
	savedSelected: string | null;
	/** Whether any selection exists server-side. */
	hasSelection: boolean;
	configText: string;
	phase: ThemePhase;
}

interface SelectionView {
	selected: boolean;
	themeKey: string | null;
	configText: string;
}

function viewOf(selected: boolean, theme: { themeId: string; version: string } | null, config: unknown): SelectionView {
	if (!selected || theme === null) {
		return { selected: false, themeKey: null, configText: '{}' };
	}
	return {
		selected: true,
		themeKey: `${theme.themeId}@${theme.version}`,
		configText: JSON.stringify(config ?? {}, null, 2)
	};
}

export class ThemeSelection {
	private state: ThemeSelectionState = {
		site: null,
		themes: [],
		selected: null,
		savedSelected: null,
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
		return (
			this.state.selected !== this.state.savedSelected ||
			this.state.configText !== this.baselineConfig
		);
	}

	private baselineConfig = '{}';

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
			const view = viewOf(
				selection.selection.selected,
				selection.selection.theme,
				selection.selection.themeConfig
			);
			this.baselineConfig = view.configText;
			this.set({
				site,
				themes,
				hasSelection: view.selected,
				savedSelected: view.themeKey,
				selected: view.themeKey,
				configText: view.configText,
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
		const submittedConfig = this.state.configText;
		const submittedSelected = this.state.selected;
		let config: unknown;
		try {
			config = JSON.parse(submittedConfig || '{}');
		} catch {
			// The form stays editable with its text; only the phase flags the
			// problem so the user can correct the JSON.
			this.set({ phase: { kind: 'error', key: 'error.validation' } });
			return;
		}
		const [themeId, version] = (submittedSelected ?? '@').split('@');
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
			const view = viewOf(
				selection.selected,
				selection.theme,
				selection.themeConfig
			);
			// Adopt the server response only where the user has not kept
			// typing: text edited during the flight stays as an unsaved
			// draft (same semantics as the settings editor).
			const next: Partial<ThemeSelectionState> = { phase: { kind: 'saved' } };
			this.baselineConfig = view.configText;
			if (this.state.selected === submittedSelected) {
				next.selected = view.themeKey;
			}
			if (this.state.configText === submittedConfig) {
				next.configText = view.configText;
			}
			next.hasSelection = view.selected;
			next.savedSelected = view.themeKey;
			this.set(next);
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

	// Conflict recovery: refresh the baseline and ETag. Draft fields the user
	// has not edited since the last adoption converge to the server values;
	// genuinely edited fields keep the user's input and stay dirty.
	async syncAfterConflict(siteId: string): Promise<void> {
		try {
			const { selection, etag } = await getSiteTheme(siteId);
			this.etag = etag;
			const view = viewOf(
				selection.selected,
				selection.theme,
				selection.themeConfig
			);
			const next: Partial<ThemeSelectionState> = {
				phase: { kind: 'conflict', synced: true }
			};
			if (this.state.selected === this.state.savedSelected) {
				next.selected = view.themeKey;
			}
			if (this.state.configText === this.baselineConfig) {
				next.configText = view.configText;
			}
			this.baselineConfig = view.configText;
			next.hasSelection = view.selected;
			next.savedSelected = view.themeKey;
			this.set(next);
		} catch {
			this.set({ phase: { kind: 'error', key: 'error.generic' } });
		}
	}
}
