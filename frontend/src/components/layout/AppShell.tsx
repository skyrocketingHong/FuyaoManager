// SideMenu's brand → information → actions → scrolling menu → footer skeleton
// from robot-platform; modules here consume the manager's own site metadata.
import { useEffect, useSyncExternalStore, type ReactNode } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { Layout, type MenuProps } from 'antd';
import {
	ArrowLeft,
	ChevronRight,
	FileText,
	Globe,
	Hammer,
	Images,
	LayoutDashboard,
	Link2,
	MapPin,
	Palette,
	Rocket,
	Settings,
	UserRound,
	type LucideIcon
} from 'lucide-react';
import { i18nSnapshot, subscribeI18n, t, type MessageKey } from '../../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../../lib/session';
import { currentAuthGeneration } from '../../lib/api/client';
import { loadSiteList, siteListSnapshot, subscribeSiteList } from '../../lib/sites/site-list';
import { itemPath, matchRoute, navItems, selectedPath, siteNavGroups } from '../../lib/navigation';
import { PageHeaderProvider } from '../PageHeader';
import SidebarActions, { type SidebarAction } from './SidebarActions';
import SidebarNav from './SidebarNav';
import SidebarInfo from './SidebarInfo';
import SidebarFoot from './SidebarFoot';
import TopBar from './TopBar';

type MenuItem = Required<MenuProps>['items'][number];
const navIcons: Record<string, LucideIcon> = {
	themes: Palette,
	'site.posts': FileText,
	'site.friends': Link2,
	'site.albums': Images,
	'site.footprints': MapPin,
	'site.settings': Settings,
	'site.config': UserRound,
	'site.theme': Palette,
	'site.builds': Hammer,
	'site.releases': Rocket
};

function menuEntry(itemId: string, siteId: string | null): MenuItem {
	const item = navItems.find((entry) => entry.id === itemId);
	if (!item) throw new Error(`unknown navigation item ${itemId}`);
	const Icon = navIcons[item.id];
	return {
		key: itemPath(item, siteId),
		label: t(item.titleKey),
		icon: Icon ? <Icon size={15} strokeWidth={1.8} /> : undefined
	};
}

export default function AppShell({ children }: { children: ReactNode }) {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const siteList = useSyncExternalStore(subscribeSiteList, siteListSnapshot);
	useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const navigate = useNavigate();
	const { pathname } = useLocation();
	const authGeneration = currentAuthGeneration();
	useEffect(() => {
		if (session.status === 'authenticated') void loadSiteList();
	}, [session.status, session.user?.id, authGeneration]);

	const match = matchRoute(pathname);
	const siteId = match?.item.scope === 'site' ? match.siteId : null;
	const currentSite = siteId ? siteList.sites.find((site) => site.siteId === siteId) : null;
	const menuItems: MenuItem[] = siteId
		? siteNavGroups.map((group) => ({
				key: group.id,
				type: 'group',
				label: t(group.titleKey),
				children: navItems
					.filter((item) => item.scope === 'site' && item.group === group.id)
					.map((item) => menuEntry(item.id, siteId))
			}))
		: [menuEntry('themes', null)];
	const actions: SidebarAction[] = [
		{
			key: 'platform-home',
			label: t('nav.platformHome'),
			icon: siteId ? <ArrowLeft size={16} /> : <LayoutDashboard size={16} strokeWidth={1.8} />,
			trailing: siteId ? undefined : <ChevronRight size={14} />,
			active: pathname === '/sites',
			onClick: () => navigate('/sites')
		}
	];
	if (siteId)
		actions.push({
			key: 'site-overview',
			label: t('nav.overview'),
			icon: <Globe size={16} strokeWidth={1.8} />,
			active: match?.item.id === 'site.overview',
			onClick: () => navigate(`/sites/${siteId}`)
		});
	const stats = siteId
		? [
				{ label: t('site.slug'), value: currentSite?.slug ?? siteId, machine: true },
				{
					label: t('sidebar.role'),
					value: currentSite ? t(`role.${currentSite.role}` as MessageKey) : '—'
				}
			]
		: [
				{
					label: t('sidebar.sites'),
					value: siteList.status === 'ready' ? String(siteList.sites.length) : '—'
				},
				{
					label: t('sidebar.editableSites'),
					value:
						siteList.status === 'ready'
							? String(siteList.sites.filter((site) => site.role !== 'viewer').length)
							: '—'
				}
			];
	const selected = selectedPath(pathname);

	return (
		<PageHeaderProvider scope={`${session.user?.id ?? 'anon'}:${pathname}`}>
			<a className="skip-workspace" href="#workspace">
				{t('app.skipWorkspace')}
			</a>
			<Layout className="app-shell">
				<Layout.Sider width={208} theme="light" className="app-sider" trigger={null}>
					<div className="sidebar-brand">
						<Link to="/sites">
							<img src="/favicon.svg" alt="" width={24} height={24} />
							<span>{t('app.title')}</span>
						</Link>
					</div>
					<SidebarInfo
						title={currentSite?.name ?? (siteId ? t('nav.overview') : t('nav.platformGroup'))}
						stats={stats}
					/>
					<SidebarActions items={actions} ariaLabel={t('nav.actions')} />
					<SidebarNav
						module={{
							items: menuItems,
							selectedKeys: selected ? [selected] : [],
							onSelect: (key) => navigate(key),
							ariaLabel: t('app.title')
						}}
					/>
					<SidebarFoot />
				</Layout.Sider>
				<Layout className="shell-main">
					<TopBar />
					<Layout.Content id="workspace" tabIndex={-1} className="shell-content">
						{children}
					</Layout.Content>
				</Layout>
			</Layout>
		</PageHeaderProvider>
	);
}
