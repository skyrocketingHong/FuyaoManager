import { useEffect } from 'react';
import type { ComponentType, ReactNode } from 'react';
import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Button, Card, Result, Spin } from 'antd';
import AppShell from './components/layout/AppShell';
import SiteBoundary from './components/layout/SiteBoundary';
import SiteRoute from './components/layout/SiteRoute';
import CaddySitePage from './pages/sites/CaddySitePage';
import ApiPage from './pages/ApiPage';
import { initLocale, t } from './lib/i18n';
import { initTheme } from './lib/theme';
import { navItems } from './lib/navigation';
import { useDocumentTitle } from './lib/title';
import { loadSession, sessionSnapshot, subscribeSession } from './lib/session';
import LoginPage from './pages/LoginPage';
import BindPasskeyPage from './pages/BindPasskeyPage';
import AccountSettingsPage from './pages/AccountSettingsPage';
import AppearancePage from './pages/AppearancePage';
import UsersPage from './pages/UsersPage';
import LogsPage from './pages/LogsPage';
import SitesPage from './pages/SitesPage';
import ThemesCatalogPage from './pages/ThemesCatalogPage';
import SiteOverviewPage from './pages/sites/SiteOverviewPage';
import ContentSourcesPage from './pages/sites/ContentSourcesPage';
import { ContentScopeProvider } from './components/ContentScopeProvider';
import type { ContentResource } from './lib/api/content';
import SettingsPage from './pages/sites/SettingsPage';
import ThemePage from './pages/sites/ThemePage';
import PostsPage from './pages/sites/PostsPage';
import FriendsPage from './pages/sites/FriendsPage';
import SiteConfigPage from './pages/sites/SiteConfigPage';
import AlbumsPage from './pages/sites/AlbumsPage';
import FootprintsPage from './pages/sites/FootprintsPage';
import BuildsPage from './pages/sites/BuildsPage';
import ReleasesPage from './pages/sites/ReleasesPage';

/** Page components keyed by navigation item id; the route table consumes
 * the same metadata as the sidebar menu, so they cannot drift apart. */
const pageByNavId: Record<string, ComponentType> = {
	'content.posts': PostsPage,
	'content.albums': AlbumsPage,
	'content.footprints': FootprintsPage,
	'content.friends': FriendsPage,
	'site.sources': ContentSourcesPage,
	'site.theme-config': () => <ThemePage view="configuration" />,
	api: ApiPage,
	'site.caddy': CaddySitePage,
	sites: SitesPage,
	themes: ThemesCatalogPage,
	'site.overview': SiteOverviewPage,
	'site.settings': SettingsPage,
	'site.config': SiteConfigPage,
	'site.posts': PostsPage,
	'site.friends': FriendsPage,
	'site.albums': AlbumsPage,
	'site.footprints': FootprintsPage,
	'site.theme': () => <ThemePage view="selection" />,
	'site.builds': BuildsPage,
	'site.releases': ReleasesPage,
	account: AccountSettingsPage,
	appearance: AppearancePage,
	users: UsersPage,
	logs: LogsPage
};

type Status = ReturnType<typeof sessionSnapshot>['status'];

/** Only anonymous browsers may see the login page. */
function AnonymousOnly({ status, children }: { status: Status; children: ReactNode }) {
	if (status === 'authenticated') return <Navigate to="/sites" replace />;
	if (status === 'restricted') return <Navigate to="/register-passkey" replace />;
	return <>{children}</>;
}

/** A restricted registration session may only reach the binding page. */
function RestrictedOnly({ status, children }: { status: Status; children: ReactNode }) {
	if (status === 'authenticated') return <Navigate to="/sites" replace />;
	if (status === 'anonymous') return <Navigate to="/login" replace />;
	return <>{children}</>;
}

/** Business surfaces require a full session; the requested URL survives the detour. */
function RequireFull({ status, children }: { status: Status; children: ReactNode }) {
	const location = useLocation();
	if (status === 'anonymous') {
		return <Navigate to="/login" replace state={{ from: location.pathname }} />;
	}
	if (status === 'restricted') {
		return <Navigate to="/register-passkey" replace state={{ from: location.pathname }} />;
	}
	return <>{children}</>;
}

export default function App() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	useDocumentTitle(session.status);

	useEffect(() => {
		initLocale();
		const stopTheme = initTheme();
		void loadSession();
		return () => stopTheme?.();
	}, []);

	if (session.status === 'loading') {
		return (
			<div className="auth-screen" role="status">
				<Spin size="large" />
			</div>
		);
	}

	if (session.status === 'error') {
		return (
			<div className="auth-screen">
				<Card className="auth-card">
					<Result
						status="warning"
						title={t('session.errorTitle')}
						subTitle={t('session.errorBody')}
						extra={
							<Button type="primary" onClick={() => void loadSession()}>
								{t('session.retry')}
							</Button>
						}
					/>
				</Card>
			</div>
		);
	}

	const status = session.status;
	return (
		<Routes>
			<Route path="/" element={<Navigate to="/sites" replace />} />
			<Route
				path="/login"
				element={
					<AnonymousOnly status={status}>
						<LoginPage />
					</AnonymousOnly>
				}
			/>
			<Route
				path="/register-passkey"
				element={
					<RestrictedOnly status={status}>
						<BindPasskeyPage />
					</RestrictedOnly>
				}
			/>
			<Route
				path="/*"
				element={
					<RequireFull status={status}>
						<AppShell>
							<Routes>
								{navItems.map((item) => {
									const Page = pageByNavId[item.id];
									return (
										<Route
											key={item.id}
											path={item.path}
											element={
												item.adminOnly && session.user?.platformRole !== 'admin' ? (
													<Result
														status="403"
														title={t('error.forbidden')}
														extra={<Button href="/sites">{t('nav.platformHome')}</Button>}
													/>
												) : item.scope === 'site' ? (
													<SiteBoundary key={item.id}>
														<SiteRoute page={item.id}>
															<Page />
														</SiteRoute>
													</SiteBoundary>
												) : item.id.startsWith('content.') ? (
													<ContentScopeProvider
														resource={item.id.slice('content.'.length) as ContentResource}
													>
														<Page />
													</ContentScopeProvider>
												) : (
													<Page />
												)
											}
										/>
									);
								})}
								<Route path="*" element={<Navigate to="/sites" replace />} />
							</Routes>
						</AppShell>
					</RequireFull>
				}
			/>
		</Routes>
	);
}
