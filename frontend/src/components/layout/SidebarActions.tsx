/**
 * SidebarActions: the fixed action region outside the scrolling menu. It
 * carries the global entries — "Platform home" always, plus "Site overview"
 * while a site scope is open — so the menu never mixes platform entries
 * into a site's workspace.
 * Ported from the robot platform's components/platform/sidebar/SidebarActions.tsx
 * (read-only reference), adapted to antd 6 and this console's i18n.
 */
import { Button } from 'antd';
import type { ReactNode } from 'react';
import Trans from '../i18n/Trans';

export interface SidebarAction {
	key: string;
	label: string;
	icon?: ReactNode;
	trailing?: ReactNode;
	active?: boolean;
	onClick: () => void;
}

export default function SidebarActions({
	items,
	ariaLabel,
	compact = false
}: {
	items: SidebarAction[];
	ariaLabel: string;
	/** Icon rail: the label span is hidden, so the name and tooltip move to the button. */
	compact?: boolean;
}) {
	return (
		<div className="sidebar-actions" aria-label={ariaLabel}>
			{items.map((item) => (
				<Button
					key={item.key}
					type="text"
					icon={item.icon}
					className={item.active ? 'is-active' : undefined}
					aria-current={item.active ? 'page' : undefined}
					aria-label={item.label}
					title={compact ? item.label : undefined}
					onClick={item.onClick}
				>
					<span className="sidebar-action-label">
						<Trans text={item.label} />
					</span>
					{item.trailing && <span className="sidebar-action-trailing">{item.trailing}</span>}
				</Button>
			))}
		</div>
	);
}
