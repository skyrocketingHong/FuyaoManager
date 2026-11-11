import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { ConfigProvider, theme as antdTheme, App as AntApp } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { useSyncExternalStore } from 'react';
import App from './App';
import { i18nSnapshot, subscribeI18n } from './lib/i18n';
import { themeSnapshot, subscribeTheme } from './lib/theme';
import './app.css';

function Root() {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const { dark } = useSyncExternalStore(subscribeTheme, themeSnapshot);
	return (
		<ConfigProvider
			locale={locale === 'zh-CN' ? zhCN : enUS}
			theme={{
				algorithm: dark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
				token: {
					colorPrimary: '#006f6b',
					borderRadius: 8,
					borderRadiusSM: 4,
					borderRadiusLG: 12
				}
			}}
		>
			<AntApp>
				<BrowserRouter>
					<App />
				</BrowserRouter>
			</AntApp>
		</ConfigProvider>
	);
}

ReactDOM.createRoot(document.getElementById('root')!).render(
	<React.StrictMode>
		<Root />
	</React.StrictMode>
);
