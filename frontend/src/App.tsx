import { useEffect } from 'react';
import type { ComponentType, ReactNode } from 'react';
import { Navigate, Route, Routes, useLocation, useParams, useSearchParams } from 'react-router-dom';
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
import { authDestination, queryChoice, requestedWorkspace, tabDestination } from './lib/navigation/query';
import { useDocumentTitle } from './lib/title';
import { loadSession, sessionSnapshot, subscribeSession } from './lib/session';
import { initTimePreferences, subscribeTime, timeSnapshot } from './lib/time';
import LoginPage from './pages/LoginPage';
import BindPasskeyPage from './pages/BindPasskeyPage';
import AccountSettingsPage from './pages/AccountSettingsPage';
import AppearancePage from './pages/AppearancePage';
import UsersPage from './pages/UsersPage';
import LogsPage from './pages/LogsPage';
import UpdatesPage from './pages/UpdatesPage';
import SitesPage from './pages/SitesPage';
import ThemesCatalogPage from './pages/ThemesCatalogPage';
import SiteOverviewPage from './pages/sites/SiteOverviewPage';
import ContentSourcesPage from './pages/sites/ContentSourcesPage';
import { ContentScopeProvider } from './components/ContentScopeProvider';
import type { ContentResource } from './lib/api/content';
import SettingsPage from './pages/sites/SettingsPage';
import ThemePage from './pages/sites/ThemePage';
import PostEditorPage from './pages/content/posts/PostEditorPage';
import PostWorkspace from './pages/content/PostWorkspace';
import { LegacyPostSectionRedirect, LegacyPostVocabularyRedirect } from './features/posts/PostWorkspaceTabs';
import FriendsPage from './pages/content/FriendsPage';
import SiteConfigPage from './pages/sites/SiteConfigPage';
import AlbumsPage from './pages/content/AlbumsPage';
import FootprintsPage from './pages/content/FootprintsPage';
import BuildsPage from './pages/sites/BuildsPage';
import ReleasesPage from './pages/sites/ReleasesPage';
import { RetryButton } from './components/ActionButton';

function ThemeWorkspace() {
	const [params] = useSearchParams();
	const view = queryChoice(params, 'tab', ['selection', 'configuration'], 'selection');
	return <ThemePage key={view} view={view} />;
}

function LegacyThemeConfiguration() {
	const { siteId } = useParams();
	const { search, hash } = useLocation();
	return <Navigate to={{ ...tabDestination(`/sites/${encodeURIComponent(siteId ?? '')}/theme`, search, 'configuration'), hash }} replace />;
}

/** Page components keyed by navigation item id; the route table consumes
 * the same metadata as the sidebar menu, so they cannot drift apart. */
const pageByNavId: Record<string, ComponentType> = {
	'content.posts': PostWorkspace,
	'content.posts.new': () => <PostEditorPage mode="new" />,
	'content.posts.edit': () => <PostEditorPage mode="edit" />,
	'content.posts.vocabulary': LegacyPostVocabularyRedirect,
	'content.posts.categories': () => <LegacyPostSectionRedirect section="categories" />,
	'content.posts.tags': () => <LegacyPostSectionRedirect section="tags" />,
	'content.posts.media': () => <LegacyPostSectionRedirect section="media" />,
	'content.albums': AlbumsPage,
	'content.footprints': FootprintsPage,
	'content.friends': FriendsPage,
	'site.sources': ContentSourcesPage,
	'site.theme-config': LegacyThemeConfiguration,
	api: ApiPage,
	'site.caddy': CaddySitePage,
	sites: SitesPage,
	themes: ThemesCatalogPage,
	'site.overview': SiteOverviewPage,
	'site.settings': SettingsPage,
	'site.config': SiteConfigPage,
	'site.posts': PostWorkspace,
	'site.posts.new': () => <PostEditorPage mode="new" />,
	'site.posts.edit': () => <PostEditorPage mode="edit" />,
	'site.posts.vocabulary': LegacyPostVocabularyRedirect,
	'site.posts.categories': () => <LegacyPostSectionRedirect section="categories" />,
	'site.posts.tags': () => <LegacyPostSectionRedirect section="tags" />,
	'site.posts.media': () => <LegacyPostSectionRedirect section="media" />,
	'site.friends': FriendsPage,
	'site.albums': AlbumsPage,
	'site.footprints': FootprintsPage,
	'site.theme': ThemeWorkspace,
	'site.builds': BuildsPage,
	'site.releases': ReleasesPage,
	account: AccountSettingsPage,
	appearance: AppearancePage,
	users: UsersPage,
	logs: LogsPage,
	updates: UpdatesPage
};

type Status = ReturnType<typeof sessionSnapshot>['status'];

/** Only anonymous browsers may see the login page. */
function AnonymousOnly({ status, children }: { status: Status; children: ReactNode }) {
	const next = requestedWorkspace(useLocation().search);
	if (status === 'authenticated') return <Navigate to={next ?? '/sites'} replace />;
	if (status === 'restricted') return <Navigate to={authDestination('/register-passkey', next)} replace />;
	return <>{children}</>;
}

/** A restricted registration session may only reach the binding page. */
function RestrictedOnly({ status, children }: { status: Status; children: ReactNode }) {
	const next = requestedWorkspace(useLocation().search);
	if (status === 'authenticated') return <Navigate to={next ?? '/sites'} replace />;
	if (status === 'anonymous') return <Navigate to={authDestination('/login', next)} replace />;
	return <>{children}</>;
}

/** Business surfaces require a full session; the requested URL survives the detour. */
function RequireFull({ status, children }: { status: Status; children: ReactNode }) {
	const location = useLocation();
	const next = location.pathname + location.search + location.hash;
	if (status === 'anonymous') {
		return <Navigate to={authDestination('/login', next)} replace />;
	}
	if (status === 'restricted') {
		return <Navigate to={authDestination('/register-passkey', next)} replace />;
	}
	return <>{children}</>;
}

export default function App() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	useSyncExternalStore(subscribeTime, timeSnapshot);
	useDocumentTitle(session.status);

	useEffect(() => {
		initLocale();
		const stopTheme = initTheme();
		const stopTime = initTimePreferences();
		void loadSession();
		return () => {
			stopTheme?.();
			stopTime();
		};
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
							<RetryButton type="primary" onClick={() => void loadSession()} />
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
														<SiteRoute page={item.parentId ?? item.id}>
															<Page />
														</SiteRoute>
													</SiteBoundary>
												) : item.id.startsWith('content.') ? (
													<ContentScopeProvider
														resource={
															(item.parentId ?? item.id).slice('content.'.length) as ContentResource
														}
														fill
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
