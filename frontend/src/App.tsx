import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Button, Card, Result, Spin } from 'antd';
import AppShell from './components/layout/AppShell';
import SiteBoundary from './components/layout/SiteBoundary';
import { initLocale, t } from './lib/i18n';
import { initTheme } from './lib/theme';
import { navItems } from './lib/navigation';
import { useDocumentTitle } from './lib/title';
import { loadSession, sessionSnapshot, subscribeSession } from './lib/session';
import LoginPage from './pages/LoginPage';
import BindPasskeyPage from './pages/BindPasskeyPage';
import SitesPage from './pages/SitesPage';
import ThemesCatalogPage from './pages/ThemesCatalogPage';
import SiteDetailPage from './pages/sites/SiteDetailPage';
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
const pageByNavId: Record<string, ReactNode> = {
	sites: <SitesPage />,
	themes: <ThemesCatalogPage />,
	'site.overview': <SiteDetailPage />,
	'site.settings': <SettingsPage />,
	'site.config': <SiteConfigPage />,
	'site.posts': <PostsPage />,
	'site.friends': <FriendsPage />,
	'site.albums': <AlbumsPage />,
	'site.footprints': <FootprintsPage />,
	'site.theme': <ThemePage />,
	'site.builds': <BuildsPage />,
	'site.releases': <ReleasesPage />
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
		initTheme();
		void loadSession();
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
									const page = pageByNavId[item.id];
									return (
										<Route
											key={item.id}
											path={item.path}
											element={
												item.scope === 'site' ? (
													<SiteBoundary>{page}</SiteBoundary>
												) : (
													page
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
