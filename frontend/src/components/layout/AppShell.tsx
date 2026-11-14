// SideMenu's brand → information → actions → scrolling menu → footer skeleton
// from robot-platform; modules here consume the manager's own site metadata.
import {
	useCallback,
	useEffect,
	useRef,
	useState,
	useSyncExternalStore,
	type ReactNode
} from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Button, Drawer, Layout, type MenuProps } from 'antd';
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
	Menu as MenuIcon,
	Palette,
	Rocket,
	Settings,
	UserRound,
	UsersRound,
	ScrollText,
	Code2,
	type LucideIcon
} from 'lucide-react';
import { i18nSnapshot, subscribeI18n, t, type MessageKey } from '../../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../../lib/session';
import { currentAuthGeneration } from '../../lib/api/client';
import { loadSiteList, siteListSnapshot, subscribeSiteList } from '../../lib/sites/site-list';
import {
	itemPath,
	matchRoute,
	navItems,
	selectedPath,
	siteNavGroups,
	platformNavGroups
} from '../../lib/navigation';
import { PageHeaderProvider } from '../PageHeader';
import SidebarContent from './SidebarContent';
import type { SidebarNavModule } from './SidebarNav';
import type { SidebarAction } from './SidebarActions';
import type { SidebarStat } from './SidebarInfo';
import TopBar from './TopBar';
import CaddyIcon from '../CaddyIcon';
import { allowSitePage } from '../../lib/navigation/workspace';

/**
 * The shell switches to the navigation drawer below 992px. This query must
 * stay in lockstep with the CSS media queries (shell.css, sidebar.css,
 * pages.css use the same 991px boundary).
 */
export const NARROW_MEDIA_QUERY = '(max-width: 991px)';

type MenuItem = Required<MenuProps>['items'][number];
const navIcons: Record<string, LucideIcon> = {
	api: Code2,
	'site.caddy': Settings,
	themes: Palette,
	appearance: Palette,
	users: UsersRound,
	logs: ScrollText,
	'site.posts': FileText,
	'site.friends': Link2,
	'site.albums': Images,
	'site.footprints': MapPin,
	'site.settings': Settings,
	'site.config': UserRound,
	'site.theme': Palette,
	'site.theme-config': Palette,
	'site.sources': Link2,
	'site.builds': Hammer,
	'site.releases': Rocket
};

function menuEntry(itemId: string, siteId: string | null): MenuItem {
	const item = navItems.find((entry) => entry.id === itemId);
	if (!item) throw new Error(`unknown navigation item ${itemId}`);
	const Icon = navIcons[item.id] ?? navIcons[item.id.replace('content.', 'site.')];
	return {
		key: itemPath(item, siteId),
		label: t(item.titleKey),
		icon:
			item.id === 'site.caddy' ? (
				<CaddyIcon />
			) : Icon ? (
				<Icon size={15} strokeWidth={1.8} />
			) : undefined
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

	// Narrow-viewport state: mirror the CSS breakpoint and follow live
	// viewport changes so the Sider and the Drawer never mount together.
	const [narrow, setNarrow] = useState(() => window.matchMedia(NARROW_MEDIA_QUERY).matches);
	useEffect(() => {
		const query = window.matchMedia(NARROW_MEDIA_QUERY);
		const onMediaChange = (event: MediaQueryListEvent) => setNarrow(event.matches);
		query.addEventListener('change', onMediaChange);
		setNarrow(query.matches);
		return () => query.removeEventListener('change', onMediaChange);
	}, []);
	// A viewport-width change always resets the drawer (a re-entered narrow
	// viewport starts closed), and a successful route change closes it —
	// blocked navigation keeps the drawer open.
	const [navOpen, setNavOpen] = useState(false);
	useEffect(() => {
		setNavOpen(false);
	}, [narrow, pathname]);
	// Focus guarantee: rc-drawer returns focus to the trigger after its leave
	// animation; the deferred focus here guarantees it even when that
	// animation is cut short. Running after the commit keeps the drawer's
	// focus trap from pulling focus back, the navOpen re-check avoids stealing
	// focus from a reopened drawer, and the unmounted ref (desktop switch)
	// keeps focus off removed nodes.
	const menuButtonRef = useRef<HTMLButtonElement | null>(null);
	const navOpenRef = useRef(false);
	navOpenRef.current = navOpen;
	const closeNavigation = useCallback(() => {
		setNavOpen(false);
		window.setTimeout(() => {
			if (!navOpenRef.current) menuButtonRef.current?.focus({ preventScroll: true });
		}, 0);
	}, []);

	const match = matchRoute(pathname);
	const siteId = match?.item.scope === 'site' ? match.siteId : null;
	const currentSite = siteId ? siteList.sites.find((site) => site.siteId === siteId) : null;
	const menuItems: MenuItem[] = [];
	for (const group of siteId ? siteNavGroups : platformNavGroups) {
		const children = navItems
			.filter(
				(item) =>
					item.scope === (siteId ? 'site' : 'platform') &&
					item.group === group.id &&
					item.menu !== false &&
					(item.scope !== 'site' || allowSitePage(currentSite, item.id)) &&
					(!item.adminOnly || session.user?.platformRole === 'admin')
			)
			.map((item) => menuEntry(item.id, siteId));
		if (children.length)
			menuItems.push({ key: group.id, type: 'group', label: t(group.titleKey), children });
	}
	const actions: SidebarAction[] = [
		{
			key: 'platform-home',
			label: t(siteId ? 'nav.returnPlatform' : 'nav.platformHome'),
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
	const stats: SidebarStat[] = siteId
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
	const navModule: SidebarNavModule = {
		items: menuItems,
		selectedKeys: selected ? [selected] : [],
		onSelect: (key) => navigate(key),
		ariaLabel: t('app.title')
	};
	const infoTitle = currentSite?.name ?? (siteId ? t('nav.overview') : t('nav.platformGroup'));

	// The drawer's own close handling (native close button, mask, Escape) is
	// covered by the rc-drawer defaults; it also restores focus to this
	// button when the drawer leaves. Unmounting on the breakpoint switch
	// cleans mask and scroll lock without touching focus.
	const menuButton = (
		<Button
			ref={menuButtonRef}
			className="topbar-icon"
			type="text"
			icon={<MenuIcon size={16} strokeWidth={1.8} />}
			aria-label={t('app.openNavigation')}
			aria-haspopup="dialog"
			aria-expanded={navOpen}
			onClick={() => setNavOpen(true)}
		/>
	);

	return (
		<PageHeaderProvider scope={`${session.user?.id ?? 'anon'}:${pathname}`}>
			<a className="skip-workspace" href="#workspace">
				{t('app.skipWorkspace')}
			</a>
			<Layout className="app-shell">
				{!narrow && (
					<Layout.Sider width={208} theme="light" className="app-sider" trigger={null}>
						<SidebarContent infoTitle={infoTitle} stats={stats} actions={actions} menu={navModule} />
					</Layout.Sider>
				)}
				<Layout className="shell-main">
					<TopBar navigationSlot={narrow ? menuButton : undefined} />
					<Layout.Content id="workspace" tabIndex={-1} className="shell-content">
						{children}
					</Layout.Content>
				</Layout>
			</Layout>
			{narrow && (
				<Drawer
					rootClassName="nav-drawer"
					placement="left"
					size={320}
					open={navOpen}
					onClose={closeNavigation}
					aria-label={t('app.navigation')}
					classNames={{ section: 'drawer-sidebar-surface', body: 'drawer-sidebar' }}
				>
					<SidebarContent
						infoTitle={infoTitle}
						stats={stats}
						actions={actions}
						menu={navModule}
						onNavigate={closeNavigation}
					/>
				</Drawer>
			)}
		</PageHeaderProvider>
	);
}
