/**
 * AppShell: desktop-app layout with a grouped sidebar (platform scope,
 * current-site scope), a sticky top bar carrying the page context and the
 * account menu, and a normally scrolling content workspace. All navigation
 * facts — routes, menu entries, selection — come from lib/navigation.
 */
import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Button, Dropdown, Layout, Menu } from 'antd';
import type { MenuProps } from 'antd';
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
import { itemPath, matchRoute, navItems, selectedPath, siteNavGroups } from '../../lib/navigation';

function GitHubMark() {
	return (
		<a
			href="https://github.com/skyrocketingHong/FuyaoManager"
			target="_blank"
			rel="noreferrer"
			aria-label="FuyaoManager on GitHub"
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

type AntdMenuItem = Required<MenuProps>['items'][number];

function menuEntry(itemId: string, siteId: string | null): AntdMenuItem {
	const item = navItems.find((entry) => entry.id === itemId);
	if (!item) throw new Error(`unknown nav item ${itemId}`);
	return {
		key: itemPath(item, siteId),
		label: <Trans text={t(item.titleKey)} />
	};
}

function menuGroup(groupKey: string, titleKey: MessageKey, children: AntdMenuItem[]): AntdMenuItem {
	return {
		key: groupKey,
		type: 'group',
		label: <Trans text={t(titleKey)} />,
		children
	};
}

export default function AppShell({ children }: { children: ReactNode }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const siteList = useSyncExternalStore(subscribeSiteList, siteListSnapshot);
	const navigate = useNavigate();
	const { pathname } = useLocation();

	useEffect(() => {
		if (session.status === 'authenticated') void loadSiteList();
	}, [session.status]);

	const other: Locale = locale === 'zh-CN' ? 'en-US' : 'zh-CN';

	// The URL is the authoritative site identity: the site-scope menu renders
	// for any /sites/:siteId/* path even while the site list is still loading.
	const match = matchRoute(pathname);
	const siteId = match && match.item.scope === 'site' ? match.siteId : null;
	const currentSite = siteId
		? (siteList.sites.find((site) => site.siteId === siteId) ?? null)
		: null;

	const platformGroup = menuGroup('platform-group', 'nav.platformGroup', [
		menuEntry('sites', null),
		menuEntry('themes', null)
	]);
	const menuItems: AntdMenuItem[] = siteId
		? [
				menuEntry('site.overview', siteId),
				...siteNavGroups
					.filter((group) => group.id !== 'overview')
					.map((group) =>
						menuGroup(
							`site-${group.id}`,
							group.titleKey,
							navItems
								.filter((item) => item.scope === 'site' && item.group === group.id)
								.map((item) => menuEntry(item.id, siteId))
						)
					),
				platformGroup
			]
		: [platformGroup];

	const selected = selectedPath(pathname);
	const titleKey: MessageKey = match ? match.item.titleKey : 'app.title';

	return (
		<Layout className="app-shell">
			<Layout.Sider width={208} theme="light" className="shell-aside" trigger={null}>
				<div className="brand-row">
					<span className="brand">
						<Trans text={t('app.title')} />
					</span>
					<GitHubMark />
				</div>
				{siteId ? (
					<div className="site-context">
						<div className="site-name">{currentSite?.name ?? siteId}</div>
						{currentSite ? (
							<div className="site-meta">
								<span>{currentSite.slug}</span>
								<span>{t(`role.${currentSite.role}` as MessageKey)}</span>
							</div>
						) : null}
					</div>
				) : null}
				<nav aria-label={t('app.title')} className="shell-nav">
					<Menu
						theme="light"
						mode="inline"
						inlineIndent={0}
						selectedKeys={selected ? [selected] : []}
						items={menuItems}
						onClick={({ key }) => navigate(String(key))}
					/>
				</nav>
			</Layout.Sider>
			<Layout className="shell-main">
				<Layout.Header className="shell-header">
					<div className="topbar-main">
						<span className="page-title">
							<Trans
								text={
									currentSite && match?.item.scope === 'site'
										? `${currentSite.name} · ${t(titleKey)}`
										: t(titleKey)
								}
							/>
						</span>
					</div>
					<div className="topbar-session">
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
							<Dropdown
								menu={{
									items: [
										{
											key: 'identity',
											label: (
												<span>
													{session.user.displayName || session.user.username}
													<span className="account-meta"> @{session.user.username}</span>
												</span>
											),
											disabled: true
										},
										{
											key: 'binding',
											label: (
												<Trans
													text={t(
														session.passkeyBound
															? 'account.passkeyBound'
															: 'account.passkeyUnbound'
													)}
												/>
											),
											disabled: true
										},
										{ type: 'divider' },
										{
											key: 'signout',
											label: <Trans text={t('app.signOut')} />,
											onClick: () => {
												void signOut().then(() => navigate('/login', { replace: true }));
											}
										}
									]
								}}
								trigger={['click']}
							>
								<Button type="text" aria-label={session.user.username}>
									<span className="account-avatar" aria-hidden>
										{(session.user.displayName || session.user.username).slice(0, 1).toUpperCase()}
									</span>
									{session.user.username}
								</Button>
							</Dropdown>
						) : null}
					</div>
				</Layout.Header>
				<Layout.Content className="shell-content">{children}</Layout.Content>
			</Layout>
		</Layout>
	);
}
