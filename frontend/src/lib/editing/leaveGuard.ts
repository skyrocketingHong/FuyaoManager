/**
 * leaveGuard: the draft confirmations active on the current page.
 *
 * `useUnsavedGuard` guards in-app navigation through the router blocker. An
 * action that ends the page without a router navigation — signing out — has
 * to ask the same question first: once the session is cleared, a cancelled
 * confirmation would leave a dirty editor mounted behind a dead session. The
 * robot platform routes sign-out through its leave gate the same way.
 */
export interface LeaveGuard {
	/** Asks the operator; resolves true once leaving is confirmed. */
	confirm: () => Promise<boolean>;
	/** Takes back a confirmed leave that did not happen after all. */
	revoke: () => void;
}

const guards = new Set<LeaveGuard>();

export function registerLeaveGuard(guard: LeaveGuard): () => void {
	guards.add(guard);
	return () => {
		guards.delete(guard);
	};
}

/** Asks every active guard in turn; a refusal cancels the earlier consents. */
export async function confirmLeave(): Promise<boolean> {
	const confirmed: LeaveGuard[] = [];
	for (const guard of [...guards]) {
		if (!(await guard.confirm())) {
			for (const done of confirmed) done.revoke();
			return false;
		}
		confirmed.push(guard);
	}
	return true;
}

/** Cancels every consent when the leave that was confirmed did not happen. */
export function revokeLeave(): void {
	for (const guard of guards) guard.revoke();
}

export function hasLeaveGuards(): boolean {
	return guards.size > 0;
}
