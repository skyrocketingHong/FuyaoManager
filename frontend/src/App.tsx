import { useEffect } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import AppShell from './components/layout/AppShell';
import { initLocale } from './lib/i18n';
import { initTheme } from './lib/theme';
import { loadSession, sessionSnapshot, subscribeSession } from './lib/session';
import LoginPage from './pages/LoginPage';
import SitesPage from './pages/SitesPage';
import SiteDetailPage from './pages/sites/SiteDetailPage';
import SettingsPage from './pages/sites/SettingsPage';
import ThemePage from './pages/sites/ThemePage';
import PostsPage from './pages/sites/PostsPage';
import FriendsPage from './pages/sites/FriendsPage';
import SiteConfigPage from './pages/sites/SiteConfigPage';
import BuildsPage from './pages/sites/BuildsPage';
import ReleasesPage from './pages/sites/ReleasesPage';

export default function App() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);

	useEffect(() => {
		initLocale();
		initTheme();
		void loadSession();
	}, []);

	if (session.status === 'loading') {
		return <p role="status">…</p>;
	}

	const routes = (
		<Routes>
			<Route path="/" element={<Navigate to="/sites" replace />} />
			<Route path="/login" element={<LoginPage />} />
			<Route path="/sites" element={<SitesPage />} />
			<Route path="/sites/:siteId" element={<SiteDetailPage />} />
			<Route path="/sites/:siteId/settings" element={<SettingsPage />} />
			<Route path="/sites/:siteId/config" element={<SiteConfigPage />} />
			<Route path="/sites/:siteId/posts" element={<PostsPage />} />
			<Route path="/sites/:siteId/friends" element={<FriendsPage />} />
			<Route path="/sites/:siteId/theme" element={<ThemePage />} />
			<Route path="/sites/:siteId/builds" element={<BuildsPage />} />
			<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
			<Route path="*" element={<Navigate to="/sites" replace />} />
		</Routes>
	);

	// The login screen stands alone; every authenticated page lives in the
	// app shell.
	return session.status === 'authenticated' ? <AppShell>{routes}</AppShell> : routes;
}
