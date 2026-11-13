import { useEffect, useSyncExternalStore } from 'react';
import { Button, Card, Result, Space } from 'antd';
import { i18nSnapshot, subscribeI18n, t } from '../lib/i18n';
import { documentTitle } from '../lib/title';

export default function RouteErrorPage() {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	useEffect(() => {
		document.title = documentTitle(t('app.pageError'));
	}, [locale]);

	return (
		<main className="auth-screen" role="alert">
			<Card className="auth-card">
				<Result
					status="error"
					title={t('app.pageError')}
					extra={
						<Space wrap>
							<Button type="primary" onClick={() => window.location.reload()}>
								{t('app.reloadPage')}
							</Button>
							<Button href="/sites">{t('nav.platformHome')}</Button>
						</Space>
					}
				/>
			</Card>
		</main>
	);
}
