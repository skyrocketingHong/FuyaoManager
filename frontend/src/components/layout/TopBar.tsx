// Adapted from robot-platform/frontend/components/platform/operationbar/TopBar.tsx.
import type { ReactNode } from 'react';
import { useSyncExternalStore } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, ConfigProvider, Dropdown, Tooltip } from 'antd';
import { ChevronsUpDown, Languages, Moon, Sun } from 'lucide-react';
import PlatformVersion from './PlatformVersion';
import { usePageHeaderContent } from '../PageHeader';
import { i18nSnapshot, setLocale, subscribeI18n, t, type Locale } from '../../lib/i18n';
import { sessionSnapshot, signOut, subscribeSession } from '../../lib/session';
import { themeSnapshot, subscribeTheme, toggleTheme } from '../../lib/theme';
import { confirmLeave, revokeLeave } from '../../lib/editing/leaveGuard';

export default function TopBar({ navigationSlot }: { navigationSlot?: ReactNode }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const navigate = useNavigate();
	const header = usePageHeaderContent();
	const other: Locale = locale === 'zh-CN' ? 'en-US' : 'zh-CN';
	const themeLabel = dark ? t('app.theme.light') : t('app.theme.dark');
	const user = session.status === 'authenticated' ? session.user : null;

	return (
		<ConfigProvider componentSize="middle">
			<header className="topbar shell-header">
				<span className="topbar-blur" aria-hidden="true">
					<i />
					<i />
					<i />
					<i />
					<i />
				</span>
				<div className="topbar-navigation">{navigationSlot}</div>
				<div className="topbar-main">
					{header?.leading && <div className="topbar-page-leading">{header.leading}</div>}
					{header?.actions && <div className="topbar-page-actions">{header.actions}</div>}
					{header?.content && <div className="topbar-page-content">{header.content}</div>}
				</div>
				<div className="topbar-session">
					<PlatformVersion />
					<Tooltip title={t('app.localeSwitch')}>
						<Button
							className="topbar-icon"
							type="text"
							icon={<Languages size={16} />}
							aria-label={other}
							onClick={() => setLocale(other)}
						/>
					</Tooltip>
					<Tooltip title={themeLabel}>
						<Button
							className="topbar-icon"
							type="text"
							icon={dark ? <Sun size={16} /> : <Moon size={16} />}
							aria-label={themeLabel}
							onClick={toggleTheme}
						/>
					</Tooltip>
					{user && (
						<Dropdown
							trigger={['click']}
							menu={{
								items: [
									// Identity lines are plain group captions, not disabled menu
									// items: screen readers must not announce them as "disabled".
									{
										key: 'identity',
										type: 'group',
										label: user.displayName || user.username,
										children: []
									},
									{
										key: 'binding',
										type: 'group',
										label: t(
											session.passkeyBound ? 'account.passkeyBound' : 'account.passkeyUnbound'
										),
										children: []
									},
									{ type: 'divider' },
									{
										key: 'appearance',
										label: t('nav.appearance'),
										onClick: () => navigate('/appearance')
									},
									{
										key: 'account-settings',
										label: t('nav.account'),
										onClick: () => navigate('/account')
									},
									{
										key: 'signout',
										label: t('app.signOut'),
										// A dirty page confirms first: the session must not be cleared
										// while its editor is still mounted and the answer may be "stay".
										onClick: () => {
											void confirmLeave().then((ok) => {
												if (!ok) return;
												signOut()
													.then(() => navigate('/login', { replace: true }))
													.catch(() => revokeLeave());
											});
										}
									}
								]
							}}
						>
							<Button className="user-button" aria-label={user.username}>
								<span className="avatar" aria-hidden="true">
									{Array.from(user.displayName || user.username)[0]}
								</span>
								<span className="operator-name">{user.username}</span>
								<ChevronsUpDown size={13} />
							</Button>
						</Dropdown>
					)}
				</div>
			</header>
		</ConfigProvider>
	);
}
