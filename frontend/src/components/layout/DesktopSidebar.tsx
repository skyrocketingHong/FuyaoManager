/**
 * DesktopSidebar: the desktop sidebar host for the expanded and icon-rail
 * modes (robot-platform's SideMenu.tsx and styles/sidebar/sidebar.css,
 * commit aa21ec10; manager-specific scope data still arrives as props).
 *
 * The icon rail keeps the 52px column. Hovering the rail or focusing any of
 * its controls floats the full menu out over the workspace without moving
 * the page. Escape dismisses the flyout while the pointer stays on the rail
 * or a control keeps keyboard focus; leaving with the pointer or focusing in
 * from outside makes it available again. The hidden mode never reaches this
 * host — the shell renders its restore button in the top bar instead.
 */
import { useEffect, useRef, useState, type FocusEvent, type ReactNode } from 'react';
import { Layout } from 'antd';
import SidebarContent, { type SidebarContentProps } from './SidebarContent';
import { sidebarWidths, type VisibleSidebarMode } from './sidebarMode';

export default function DesktopSidebar({
	mode,
	modeButton,
	...content
}: { mode: VisibleSidebarMode; modeButton: ReactNode } & SidebarContentProps) {
	const panel = useRef<HTMLDivElement>(null);
	const [peekDismissed, setPeekDismissed] = useState(false);
	useEffect(() => {
		if (mode !== 'icons') return;
		const dismiss = (event: KeyboardEvent) => {
			if (
				event.key === 'Escape' &&
				!event.defaultPrevented &&
				panel.current?.matches(':hover, :has(:focus-visible)')
			)
				setPeekDismissed(true);
		};
		window.addEventListener('keydown', dismiss);
		return () => window.removeEventListener('keydown', dismiss);
	}, [mode]);
	const dismissOnLeave = () => setPeekDismissed(false);
	const keepOnInnerFocus = (event: FocusEvent<HTMLDivElement>) => {
		if (!event.currentTarget.contains(event.relatedTarget)) setPeekDismissed(false);
	};

	return (
		<Layout.Sider
			width={sidebarWidths[mode]}
			theme="light"
			className="app-sider"
			trigger={null}
			data-mode={mode}
			data-peek-dismissed={peekDismissed || undefined}
			onMouseLeave={dismissOnLeave}
		>
			<div className="sidebar-panel" ref={panel} onFocus={keepOnInnerFocus}>
				<SidebarContent modeButton={modeButton} compact={mode === 'icons'} {...content} />
			</div>
		</Layout.Sider>
	);
}
