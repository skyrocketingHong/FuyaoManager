import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, Button, Card, Typography } from 'antd';
import { loginWithPasskey } from '../lib/api/passkey';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { markSignedIn } from '../lib/session';

export default function LoginPage() {
	const navigate = useNavigate();
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	async function handleLogin() {
		setBusy(true);
		setErrorKey(null);
		try {
			const done = await loginWithPasskey();
			markSignedIn(done.user);
			navigate('/sites');
		} catch (err) {
			setErrorKey(apiErrorKey(err));
		} finally {
			setBusy(false);
		}
	}

	return (
		<Card title={t('login.title')} style={{ maxWidth: 420, margin: '48px auto' }}>
			<Typography.Paragraph>{t('login.description')}</Typography.Paragraph>
			{errorKey ? <Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 16 }} /> : null}
			<Button type="primary" block loading={busy} onClick={handleLogin}>
				{t('login.action')}
			</Button>
			<Typography.Paragraph style={{ marginTop: 16, marginBottom: 0 }}>
				<a href="/bootstrap">{t('login.firstTime')}</a>
			</Typography.Paragraph>
		</Card>
	);
}
