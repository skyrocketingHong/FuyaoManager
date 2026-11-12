/**
 * AppearanceProvider: one computation feeds both the antd ConfigProvider
 * theme and the semantic CSS custom properties, so component tokens and
 * hand-written shell styles never drift apart. The mode comes from the
 * persisted day/night store; locale feeds the antd locale pack.
 */
import { useLayoutEffect, useMemo, type ReactNode } from 'react';
import { useSyncExternalStore } from 'react';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { i18nSnapshot, subscribeI18n } from '../lib/i18n';
import { subscribeTheme, themeSnapshot } from '../lib/theme';
import { appearanceTheme } from '../lib/theme/appearance';

export default function AppearanceProvider({ children }: { children: ReactNode }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const mode = dark ? 'dark' : 'light';
	const appearance = useMemo(() => appearanceTheme(mode), [mode]);

	useLayoutEffect(() => {
		const root = document.documentElement;
		root.classList.toggle('dark', mode === 'dark');
		root.style.colorScheme = mode;
		for (const [name, value] of Object.entries(appearance.variables)) {
			root.style.setProperty(name, value);
		}
	}, [appearance, mode]);

	return (
		<ConfigProvider locale={locale === 'zh-CN' ? zhCN : enUS} theme={appearance.config}>
			<AntApp>{children}</AntApp>
		</ConfigProvider>
	);
}
