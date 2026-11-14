/**
 * SidebarContent: the brand → info → fixed actions → scrolling menu → foot
 * regions as one reusable surface. The desktop Sider and the narrow-screen
 * navigation Drawer render this same tree, so navigation metadata, permission
 * filtering, and site context stay single-sourced (ported region layout from
 * the robot platform's SideMenu skeleton, read-only reference).
 */
import { Link } from 'react-router-dom';
import { t } from '../../lib/i18n';
import SidebarNav, { type SidebarNavModule } from './SidebarNav';
import SidebarActions, { type SidebarAction } from './SidebarActions';
import SidebarInfo, { type SidebarStat } from './SidebarInfo';
import SidebarFoot from './SidebarFoot';

export default function SidebarContent({
	infoTitle,
	stats,
	actions,
	menu,
	onNavigate
}: {
	infoTitle: string;
	stats: SidebarStat[];
	actions: SidebarAction[];
	menu: SidebarNavModule;
	/** Invoked after any navigation interaction: menu, fixed action, brand. */
	onNavigate?: () => void;
}) {
	return (
		<div className="sidebar-content">
			<div className="sidebar-brand">
				<Link to="/sites" onClick={onNavigate}>
					<img src="/favicon.svg" alt="" width={24} height={24} />
					<span>{t('app.title')}</span>
				</Link>
			</div>
			<SidebarInfo title={infoTitle} stats={stats} />
			<SidebarActions
				items={actions.map((action) => ({
					...action,
					onClick: () => {
						action.onClick();
						onNavigate?.();
					}
				}))}
				ariaLabel={t('nav.actions')}
			/>
			<SidebarNav
				module={{
					...menu,
					onSelect: (key) => {
						menu.onSelect(key);
						onNavigate?.();
					}
				}}
			/>
			<SidebarFoot />
		</div>
	);
}
