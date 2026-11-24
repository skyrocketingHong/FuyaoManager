// Desktop sidebar modes from robot-platform/frontend/components/platform/
// sidebar/sidebarMode.ts and styles/shell.css (commit aa21ec10): the column is
// 208px expanded, 52px as an icon rail, or hidden with its restore button in
// the top bar. The choice persists per browser.
export type SidebarMode = 'expanded' | 'icons' | 'hidden';
export type VisibleSidebarMode = Exclude<SidebarMode, 'hidden'>;

export const sidebarModeStorageKey = 'fuyao-manager-sidebar:v1';
export const sidebarWidths: Record<VisibleSidebarMode, number> = { expanded: 208, icons: 52 };

const nextMode: Record<SidebarMode, SidebarMode> = { expanded: 'icons', icons: 'hidden', hidden: 'expanded' };

export function readSidebarMode(): SidebarMode {
	try {
		const saved = localStorage.getItem(sidebarModeStorageKey);
		if (saved === 'icons' || saved === 'hidden') return saved;
	} catch {
		/* The sidebar can still be changed for this browser session. */
	}
	return 'expanded';
}

export function changeSidebarMode(mode: SidebarMode): SidebarMode {
	const next = nextMode[mode];
	try {
		localStorage.setItem(sidebarModeStorageKey, next);
	} catch {
		/* The current mode remains usable without persistent storage. */
	}
	return next;
}
