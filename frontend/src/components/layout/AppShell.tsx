/**
 * AppShell: top bar with brand, locale switch, day/night toggle, the signed-in
 * user, sign-out, and the project's GitHub mark (Simple Icons). Children are
 * the routed pages.
 */
import type { ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Button, Space, Tooltip } from 'antd';
import { siGithub } from 'simple-icons';
import Trans from '../i18n/Trans';
import { i18nSnapshot, setLocale, subscribeI18n, t, type Locale } from '../../lib/i18n';
import { themeSnapshot, subscribeTheme, toggleTheme } from '../../lib/theme';
import { sessionSnapshot, signOut, subscribeSession } from '../../lib/session';

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

export default function AppShell({ children }: { children: ReactNode }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const navigate = useNavigate();

	const other: Locale = locale === 'zh-CN' ? 'en-US' : 'zh-CN';

	return (
		<div className="shell">
			<header>
				<Space wrap size={[16, 8]} style={{ padding: '12px 16px' }}>
					<span className="brand"><Trans text={t('app.title')} /></span>
					<GitHubMark />
					<span style={{ flex: 1 }} />
					<Button type="text" onClick={() => setLocale(other)} aria-label={other}>
						<Trans text={t('app.localeSwitch')} />
					</Button>
					<Button type="text" onClick={toggleTheme}>
						<Trans key={dark ? 'd' : 'l'} text={dark ? t('app.theme.light') : t('app.theme.dark')} />
					</Button>
					{session.status === 'authenticated' && session.user ? (
						<>
							<Tooltip title={session.user.displayName}>
								<span>{session.user.username}</span>
							</Tooltip>
							<Button
								type="text"
								onClick={async () => {
									await signOut();
									navigate('/login');
								}}
							>
								<Trans text={t('app.signOut')} />
							</Button>
						</>
					) : null}
				</Space>
			</header>
			<main>{children}</main>
		</div>
	);
}
