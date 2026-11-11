import { useEffect } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import AppShell from './components/layout/AppShell';
import { initLocale } from './lib/i18n';
import { initTheme } from './lib/theme';
import { loadSession, sessionSnapshot, subscribeSession } from './lib/session';
import LoginPage from './pages/LoginPage';
import BootstrapPage from './pages/BootstrapPage';
import SitesPage from './pages/SitesPage';
import SiteDetailPage from './pages/sites/SiteDetailPage';
import SettingsPage from './pages/sites/SettingsPage';
import ThemePage from './pages/sites/ThemePage';
import BuildsPage from './pages/sites/BuildsPage';
import ReleasesPage from './pages/sites/ReleasesPage';

export default function App() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);

	useEffect(() => {
		initLocale();
		initTheme();
		void loadSession();
	}, []);

	return (
		<AppShell>
			{session.status === 'loading' ? (
				<p role="status">…</p>
			) : (
				<Routes>
					<Route path="/" element={<Navigate to="/sites" replace />} />
					<Route path="/login" element={<LoginPage />} />
					<Route path="/bootstrap" element={<BootstrapPage />} />
					<Route path="/sites" element={<SitesPage />} />
					<Route path="/sites/:siteId" element={<SiteDetailPage />} />
					<Route path="/sites/:siteId/settings" element={<SettingsPage />} />
					<Route path="/sites/:siteId/theme" element={<ThemePage />} />
					<Route path="/sites/:siteId/builds" element={<BuildsPage />} />
					<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
					<Route path="*" element={<Navigate to="/sites" replace />} />
				</Routes>
			)}
		</AppShell>
	);
}
