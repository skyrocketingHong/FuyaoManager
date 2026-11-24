/**
 * SidebarContent: the brand → info → fixed actions → scrolling menu → foot
 * regions as one reusable surface. The desktop Sider and the narrow-screen
 * navigation Drawer render this same tree, so navigation metadata, permission
 * filtering, and site context stay single-sourced (ported region layout from
 * the robot platform's SideMenu skeleton, read-only reference).
 *
 * This component is purely presentational and never guesses whether a
 * navigation succeeds: `menu` and `actions` arrive with their close
 * semantics already wired by the caller, and `onBrandClick` receives the
 * brand link's click event for the same purpose.
 */
import type { MouseEvent, ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { t } from '../../lib/i18n';
import SidebarNav, { type SidebarNavModule } from './SidebarNav';
import SidebarActions, { type SidebarAction } from './SidebarActions';
import SidebarInfo, { type SidebarStat } from './SidebarInfo';
import SidebarFoot from './SidebarFoot';

export interface SidebarContentProps {
	infoTitle: string;
	stats: SidebarStat[];
	actions: SidebarAction[];
	menu: SidebarNavModule;
	/** Brand link click handler; routing decisions stay with the caller. */
	onBrandClick?: (event: MouseEvent<HTMLAnchorElement>) => void;
	/** Sidebar mode control rendered before the brand; desktop host only. */
	modeButton?: ReactNode;
	/** Icon rail: labels are hidden, so controls also carry a title tooltip. */
	compact?: boolean;
}

export default function SidebarContent({
	infoTitle,
	stats,
	actions,
	menu,
	onBrandClick,
	modeButton,
	compact = false
}: SidebarContentProps) {
	return (
		<div className="sidebar-content">
			<div className="sidebar-brand">
				{modeButton}
				<Link to="/sites" onClick={onBrandClick}>
					<img src="/favicon.svg" alt="" width={24} height={24} />
					<span>{t('app.title')}</span>
				</Link>
			</div>
			<SidebarInfo title={infoTitle} stats={stats} />
			<SidebarActions items={actions} ariaLabel={t('nav.actions')} compact={compact} />
			<SidebarNav module={menu} />
			<SidebarFoot />
		</div>
	);
}
