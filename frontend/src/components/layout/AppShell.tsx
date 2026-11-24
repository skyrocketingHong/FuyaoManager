import { observeScrollEdges } from '../../lib/ui/scrollFade';
// SideMenu's brand → location → actions → scrolling menu → footer skeleton
// from robot-platform; modules here consume the manager's own site metadata.
import {
	useCallback,
	useEffect,
	useRef,
	useState,
	useSyncExternalStore,
	type MouseEvent,
	type ReactNode
} from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Button, Drawer, Layout, type MenuProps } from 'antd';
import {
	ArrowLeft,
	ChevronRight,
	Globe,
	Hammer,
	LayoutDashboard,
	Link2,
	Menu as MenuIcon,
	Palette,
	PanelLeftClose,
	PanelLeftDashed,
	PanelLeftOpen,
	Rocket,
	Settings,
	UserRound,
	UsersRound,
	ScrollText,
	Code2,
	type LucideIcon
} from 'lucide-react';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';
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
import TopBar from './TopBar';
import CaddyIcon from '../CaddyIcon';
import DesktopSidebar from './DesktopSidebar';
import { changeSidebarMode, readSidebarMode } from './sidebarMode';
import SidebarMotionIcon, { replaySidebarMotionIcon, type SidebarMotionIconName } from './SidebarMotionIcon';
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
	updates: Rocket,
	'site.settings': Settings,
	'site.config': UserRound,
	'site.theme': Palette,
	'site.theme-config': Palette,
	'site.sources': Link2,
	'site.builds': Hammer,
	'site.releases': Rocket
};

const contentIcons: Partial<Record<string, SidebarMotionIconName>> = {
	'site.posts': 'posts',
	'site.albums': 'albums',
	'site.footprints': 'footprints',
	'site.friends': 'friends'
};

function menuEntry(itemId: string, siteId: string | null, compact: boolean): MenuItem {
	const item = navItems.find((entry) => entry.id === itemId);
	if (!item) throw new Error(`unknown navigation item ${itemId}`);
	const iconId = item.id.replace(/^content\./, 'site.');
	const motionIcon = contentIcons[iconId];
	// The icon rail hides the label node, so the item keeps its name through
	// aria-label and gains a native tooltip only while the rail is shown.
	const label = t(item.titleKey);
	const entry = {
		key: itemPath(item, siteId),
		label,
		'aria-label': label,
		title: compact ? label : undefined
	};
	if (motionIcon) {
		return {
			...entry,
			icon: <SidebarMotionIcon name={motionIcon} />,
			onClick: ({ domEvent }) => replaySidebarMotionIcon(domEvent.currentTarget, motionIcon)
		};
	}
	const Icon = navIcons[iconId];
	return {
		...entry,
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
	const workspaceRef = useRef<HTMLElement>(null);
	const article = /^\/(?:sites\/[^/]+|content)\/posts(?:\/|$)/.test(pathname);
	useEffect(() => {
		const node = workspaceRef.current;
		if (node && !article) return observeScrollEdges(node);
	}, [article, pathname]);
	const authGeneration = currentAuthGeneration();
	useEffect(() => {
		if (session.status === 'authenticated') void loadSiteList();
	}, [session.status, session.user?.id, authGeneration]);

	// Desktop sidebar mode (expanded → icons → hidden), persisted per browser.
	// The control moves between the sidebar brand row (expanded and icons)
	// and the top bar's navigation slot (hidden); focus follows it so the
	// three-state toggle stays operable from the keyboard in every mode.
	const [sidebarMode, setSidebarMode] = useState(readSidebarMode);
	const sidebarButtonRef = useRef<HTMLButtonElement>(null);
	const restoreSidebarFocus = useRef(false);
	useEffect(() => {
		if (!restoreSidebarFocus.current) return;
		restoreSidebarFocus.current = false;
		sidebarButtonRef.current?.focus({ preventScroll: true });
	}, [sidebarMode]);
	const sidebarAction =
		sidebarMode === 'hidden'
			? t('app.expandSidebar')
			: sidebarMode === 'icons'
				? t('app.hideSidebar')
				: t('app.collapseSidebar');
	const SidebarGlyph =
		sidebarMode === 'hidden' ? PanelLeftOpen : sidebarMode === 'icons' ? PanelLeftDashed : PanelLeftClose;
	const sidebarButton = (
		<button
			ref={sidebarButtonRef}
			type="button"
			className="sidebar-mode-button"
			aria-label={sidebarAction}
			title={sidebarAction}
			onClick={() => {
				restoreSidebarFocus.current = true;
				setSidebarMode(changeSidebarMode(sidebarMode));
			}}
		>
			<SidebarGlyph size={18} strokeWidth={1.8} />
		</button>
	);

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
	const closeNavigation = useCallback(() => setNavOpen(false), []);

	// One navigation entry point for the menu, the fixed actions, and the
	// brand link: an already-current target closes the drawer directly (no
	// pointless navigation, no leave confirmation), while a cross-page target
	// navigates and lets the pathname effect above close the drawer only
	// after the route actually changes — a blocked navigation keeps it open.
	const navigateWorkspace = useCallback(
		(target: string) => {
			if (target === pathname) {
				setNavOpen(false);
				return;
			}
			navigate(target);
		},
		[navigate, pathname]
	);

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
			.map((item) => menuEntry(item.id, siteId, sidebarMode === 'icons'));
		if (children.length)
			menuItems.push({
				key: group.id,
				type: 'group',
				// The text sits in its own box so the icon rail can clamp it to
				// two lines without changing the title's height.
				label: <span className="sidebar-group-text">{t(group.titleKey)}</span>,
				children
			});
	}
	const actions: SidebarAction[] = [
		{
			key: 'platform-home',
			label: t(siteId ? 'nav.returnPlatform' : 'nav.platformHome'),
			icon: siteId ? <ArrowLeft size={16} /> : <LayoutDashboard size={16} strokeWidth={1.8} />,
			trailing: siteId ? undefined : <ChevronRight size={14} />,
			active: pathname === '/sites',
			onClick: () => navigateWorkspace('/sites')
		}
	];
	if (siteId)
		actions.push({
			key: 'site-overview',
			label: t('nav.overview'),
			icon: <Globe size={16} strokeWidth={1.8} />,
			active: match?.item.id === 'site.overview',
			onClick: () => navigateWorkspace(`/sites/${siteId}`)
		});
	const selected = selectedPath(pathname);
	const navModule: SidebarNavModule = {
		items: menuItems,
		selectedKeys: selected ? [selected] : [],
		onSelect: (key) => navigateWorkspace(key),
		ariaLabel: t('app.title')
	};
	// The site's name heads the menu in a site scope; until the site list
	// names it, the scope's overview entry stands in.
	const location = currentSite?.name ?? (siteId ? t('nav.overview') : undefined);

	// The brand anchor keeps its href; routing decisions live here. Only an
	// unmodified primary click on the current page is intercepted (close the
	// drawer without navigating, so a dirty page never gets a pointless leave
	// confirmation) — Ctrl/Cmd/Shift/Alt and non-primary buttons fall through
	// to the browser (new tab etc.) with the drawer untouched. Cross-page
	// plain clicks keep the anchor's default navigation, and the drawer
	// closes via the pathname effect only after the route actually changes.
	const handleBrandClick = useCallback(
		(event: MouseEvent<HTMLAnchorElement>) => {
			if (
				pathname === '/sites' &&
				event.button === 0 &&
				!event.ctrlKey &&
				!event.metaKey &&
				!event.shiftKey &&
				!event.altKey
			) {
				event.preventDefault();
				setNavOpen(false);
			}
		},
		[pathname]
	);

	// The drawer's native focus lifecycle covers open and leave: rc-drawer
	// captures the opener during the open commit (layout phase, before the
	// autofocus) and restores it when the drawer leaves; the focus trap walks
	// Tab into the content; unmounting on the breakpoint switch cleans mask
	// and scroll lock without touching focus.
	const menuButton = (
		<Button
			className="topbar-icon"
			type="text"
			icon={<MenuIcon size={16} strokeWidth={1.8} />}
			aria-label={t('app.openNavigation')}
			aria-haspopup="dialog"
			aria-expanded={navOpen}
			onClick={() => setNavOpen(true)}
		/>
	);

	// Top-bar navigation slot: the drawer trigger on narrow screens. On the
	// desktop it stays empty while the sidebar is expanded, carries the
	// restore control once the sidebar is hidden, and shows the brand mark
	// beside the icon rail (the rail itself displays icons only).
	const navigationSlot = narrow
		? menuButton
		: sidebarMode === 'hidden'
			? sidebarButton
			: sidebarMode === 'icons'
				? (
						<img
							className="topbar-brand-mark"
							src="/favicon.svg"
							alt={t('app.title')}
							width={28}
							height={28}
						/>
					)
				: undefined;

	return (
		<PageHeaderProvider scope={`${session.user?.id ?? 'anon'}:${pathname}`}>
			<a className="skip-workspace" href="#workspace">
				{t('app.skipWorkspace')}
			</a>
			<Layout className="app-shell" data-sidebar={sidebarMode}>
				{!narrow && sidebarMode !== 'hidden' && (
					<DesktopSidebar
						mode={sidebarMode}
						modeButton={sidebarButton}
						location={location}
						actions={actions}
						menu={navModule}
					/>
				)}
				<Layout className="shell-main">
					<TopBar navigationSlot={navigationSlot} />
					<Layout.Content id="workspace" ref={workspaceRef} tabIndex={-1} className="shell-content" data-workspace={article ? 'article' : 'console'}>
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
						location={location}
						actions={actions}
						menu={navModule}
						onBrandClick={handleBrandClick}
					/>
				</Drawer>
			)}
		</PageHeaderProvider>
	);
}
