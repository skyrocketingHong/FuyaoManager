/**
 * EditableDoc: one shared editing-state machine for document editors
 * (friends, footprints, albums, site config) and the pattern reference for
 * form-based editors (posts). It separates three versions of a document:
 *
 * - baseline: the last server-confirmed state plus its ETag
 * - draft: the user's current working copy (null = identical to baseline)
 * - submitted: the snapshot captured when a save began
 *
 * Invariants enforced here (R5):
 * - a successful save confirms only its submitted snapshot: if the user kept
 *   editing during the request, those newer edits stay in the draft and the
 *   editor stays dirty
 * - conflict sync updates the baseline and ETag only; local modifications
 *   are preserved (discarding them is a separate, explicit action)
 * - every transition is epoch-tagged by the caller, so late responses from a
 *   previous site, object, or session cannot mutate current state
 */
export interface EditableState<T> {
	baseline: T | null;
	draft: T | null;
	etag: string;
	saving: boolean;
	conflict: boolean;
	loading: boolean;
}

export type EditableListener = () => void;

export class EditableDoc<T> {
	private state: EditableState<T> = {
		baseline: null,
		draft: null,
		etag: '',
		saving: false,
		conflict: false,
		loading: true
	};
	private listeners = new Set<EditableListener>();
	private submitted: T | null = null;

	subscribe = (listener: EditableListener): (() => void) => {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	};

	getSnapshot = (): EditableState<T> => this.state;

	private emit(): void {
		for (const listener of this.listeners) listener();
	}

	private set(patch: Partial<EditableState<T>>): void {
		this.state = { ...this.state, ...patch };
		this.emit();
	}

	/** True when the draft differs from the baseline (deep compare by JSON). */
	isDirty(): boolean {
		if (this.state.draft === null) return false;
		if (this.state.baseline === null) return true;
		return JSON.stringify(this.state.draft) !== JSON.stringify(this.state.baseline);
	}

	/** Loads a server document as the new baseline; drops local state. */
	load(doc: T, etag: string): void {
		this.submitted = null;
		this.set({ baseline: doc, draft: null, etag, saving: false, conflict: false, loading: false });
	}

	markLoading(): void {
		this.set({ loading: true });
	}

	/** Applies a local edit to the draft (creating it from the baseline). */
	mutate(updater: (current: T) => T): void {
		const source = this.state.draft ?? this.state.baseline;
		if (source === null) return;
		this.set({ draft: updater(source), conflict: false });
	}

	/** Replaces the whole draft (used by editors holding row arrays). */
	setDraft(draft: T): void {
		this.set({ draft, conflict: false });
	}

	/** Captures the save snapshot. Returns null when there is nothing to save. */
	beginSave(): T | null {
		const snapshot = this.state.draft ?? this.state.baseline;
		if (snapshot === null) return null;
		this.submitted = snapshot;
		this.set({ saving: true });
		return snapshot;
	}

	/**
	 * Resolves a successful save: the baseline becomes the server-confirmed
	 * document. If the draft moved past the submitted snapshot (the user kept
	 * editing during the request), it survives and the editor stays dirty.
	 */
	resolveSave(saved: T, etag: string): void {
		const keptDraft =
			this.state.draft !== null &&
			JSON.stringify(this.state.draft) !== JSON.stringify(this.submitted)
				? this.state.draft
				: null;
		this.submitted = null;
		this.set({ baseline: saved, draft: keptDraft, etag, saving: false, conflict: false });
	}

	/** Resolves a failed save; conflict=true shows the recovery banner. */
	rejectSave(conflict: boolean): void {
		this.submitted = null;
		this.set({ saving: false, conflict });
	}

	/**
	 * Conflict sync: adopt the server version as the new baseline and ETag
	 * while keeping the local draft untouched — local edits are never
	 * silently discarded.
	 */
	syncAfterConflict(server: T, etag: string): void {
		this.set({ baseline: server, etag, conflict: false });
	}

	/** Explicit discard: drop the local draft and return to the baseline. */
	discardLocal(): void {
		this.set({ draft: null, conflict: false });
	}
}

/** Extracts a raw row-array draft idiom used by list editors. */
export function replaceAt<T>(items: T[], index: number, value: T): T[] {
	return items.map((item, i) => (i === index ? value : item));
}
