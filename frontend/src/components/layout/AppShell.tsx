/**
 * AppShell: desktop-app layout with a grouped sidebar (platform scope,
 * current-site scope), a sticky top bar carrying the page title and account
 * actions, and a normally scrolling content workspace. Brand mark from
 * Simple Icons; everything else is stock Ant Design.
 */
import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Button, Layout, Menu, Space } from 'antd';
import { siGithub } from 'simple-icons';
import Trans from '../i18n/Trans';
import {
	i18nSnapshot,
	setLocale,
	subscribeI18n,
	t,
	type Locale,
	type MessageKey
} from '../../lib/i18n';
import { themeSnapshot, subscribeTheme, toggleTheme } from '../../lib/theme';
import { sessionSnapshot, signOut, subscribeSession } from '../../lib/session';
import { loadSiteList, siteListSnapshot, subscribeSiteList } from '../../lib/sites/site-list';

function GitHubMark() {
	return (
		<a
			href="https://github.com/skyrocketingHong/FuyaoHomepage"
			target="_blank"
			rel="noreferrer"
			aria-label="FuyaoHomepage on GitHub"
			style={{ color: 'inherit', display: 'inline-flex', alignItems: 'center' }}
		>
			<svg
				width="18"
				height="18"
				viewBox="0 0 24 24"
				role="img"
				aria-hidden="true"
				fill="currentColor"
			>
				<path d={siGithub.path} />
			</svg>
		</a>
	);
}

type SiteSection =
	| 'overview'
	| 'settings'
	| 'config'
	| 'theme'
	| 'posts'
	| 'friends'
	| 'builds'
	| 'releases';

const siteSections: { key: SiteSection; path: string }[] = [
	{ key: 'overview', path: '' },
	{ key: 'settings', path: '/settings' },
	{ key: 'config', path: '/config' },
	{ key: 'posts', path: '/posts' },
	{ key: 'friends', path: '/friends' },
	{ key: 'theme', path: '/theme' },
	{ key: 'builds', path: '/builds' },
	{ key: 'releases', path: '/releases' }
];

/** Resolves the current route into (siteId, section) and the page title key. */
function useRouteScope(): {
	siteId: string | null;
	section: SiteSection | null;
	titleKey: MessageKey;
} {
	const { pathname } = useLocation();
	const siteMatch = pathname.match(
		/^\/sites\/([^/]+)(\/(settings|config|posts|friends|theme|builds|releases))?\/?$/
	);
	if (siteMatch) {
		const section = (siteMatch[3] as SiteSection | undefined) ?? 'overview';
		return { siteId: siteMatch[1], section, titleKey: `nav.${section}` as MessageKey };
	}
	if (pathname === '/sites')
		return { siteId: null, section: null, titleKey: 'nav.sites' as MessageKey };
	return { siteId: null, section: null, titleKey: 'app.title' as MessageKey };
}

export default function AppShell({ children }: { children: ReactNode }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const siteList = useSyncExternalStore(subscribeSiteList, siteListSnapshot);
	const navigate = useNavigate();
	const scope = useRouteScope();

	useEffect(() => {
		if (session.status === 'authenticated') void loadSiteList();
	}, [session.status]);

	const other: Locale = locale === 'zh-CN' ? 'en-US' : 'zh-CN';
	const currentSite = scope.siteId
		? (siteList.sites.find((s) => s.siteId === scope.siteId) ?? null)
		: null;

	const menuItems =
		scope.siteId && currentSite
			? [
					{ label: <Trans text={t('nav.siteGroup')} />, key: 'site-group', type: 'group' as const },
					...siteSections.map((s) => ({
						label: <Trans text={t(`nav.${s.key}` as MessageKey)} />,
						key: `/sites/${scope.siteId}${s.path}`
					})),
					{
						label: <Trans text={t('nav.platformGroup')} />,
						key: 'platform-group',
						type: 'group' as const
					},
					{ label: <Trans text={t('nav.sites')} />, key: '/sites' }
				]
			: [
					{
						label: <Trans text={t('nav.platformGroup')} />,
						key: 'platform-group',
						type: 'group' as const
					},
					{ label: <Trans text={t('nav.sites')} />, key: '/sites' }
				];

	const selectedKey = scope.siteId
		? scope.section === 'overview'
			? `/sites/${scope.siteId}`
			: `/sites/${scope.siteId}/${scope.section}`
		: '/sites';

	return (
		<Layout style={{ minHeight: '100vh' }}>
			<Layout.Sider
				width={232}
				theme="dark"
				className="shell-aside"
				breakpoint="lg"
				collapsedWidth={72}
			>
				<div className="brand-row">
					<span className="brand">
						<Trans text={t('app.title')} />
					</span>
					<GitHubMark />
				</div>
				{currentSite ? (
					<div className="site-context">
						<div className="site-name">{currentSite.name}</div>
						<div className="site-meta">
							<span>{currentSite.slug}</span>
						</div>
					</div>
				) : null}
				<nav aria-label={t('app.title')}>
					<Menu
						theme="dark"
						mode="inline"
						selectedKeys={[selectedKey]}
						items={menuItems}
						onClick={({ key }) => navigate(String(key))}
					/>
				</nav>
			</Layout.Sider>
			<Layout className="shell-main">
				<Layout.Header className="shell-header">
					<span className="page-title">
						<Trans
							text={
								currentSite && scope.section
									? `${currentSite.name} · ${t(`nav.${scope.section}` as MessageKey)}`
									: t(scope.titleKey)
							}
						/>
					</span>
					<span style={{ flex: 1 }} />
					<Space wrap size={[4, 8]}>
						<Button type="text" onClick={() => setLocale(other)} aria-label={other}>
							<Trans text={t('app.localeSwitch')} />
						</Button>
						<Button type="text" onClick={toggleTheme}>
							<Trans
								key={dark ? 'd' : 'l'}
								text={dark ? t('app.theme.light') : t('app.theme.dark')}
							/>
						</Button>
						{session.status === 'authenticated' && session.user ? (
							<Button
								type="text"
								onClick={async () => {
									await signOut();
									navigate('/login');
								}}
								aria-label={`${session.user.username}: ${t('app.signOut')}`}
							>
								<Trans text={`${session.user.username} · ${t('app.signOut')}`} />
							</Button>
						) : null}
					</Space>
				</Layout.Header>
				<Layout.Content className="shell-content">{children}</Layout.Content>
			</Layout>
		</Layout>
	);
}
