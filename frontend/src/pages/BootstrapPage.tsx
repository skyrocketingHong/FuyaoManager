import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, Button, Card, Form, Input, Typography } from 'antd';
import { enrollPasskey } from '../lib/api/passkey';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { markSignedIn } from '../lib/session';

export default function BootstrapPage() {
	const navigate = useNavigate();
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [permit, setPermit] = useState('');

	async function handleEnroll(value: string) {
		const trimmed = value.trim();
		if (trimmed === '') return;
		setBusy(true);
		setErrorKey(null);
		try {
			const done = await enrollPasskey(trimmed);
			markSignedIn(done.user);
			navigate('/sites');
		} catch (err) {
			setErrorKey(apiErrorKey(err));
		} finally {
			setBusy(false);
		}
	}

	return (
		<Card title={t('bootstrap.title')} style={{ maxWidth: 420, margin: '48px auto' }}>
			<Typography.Paragraph>{t('bootstrap.description')}</Typography.Paragraph>
			<Form onFinish={(values) => handleEnroll(values.permit as string)}>
				<Form.Item
					name="permit"
					label={t('bootstrap.permitLabel')}
					extra={t('bootstrap.permitHint')}
					rules={[{ required: true }]}
				>
					<Input
						value={permit}
						onChange={(e) => setPermit(e.target.value)}
						autoComplete="off"
						spellCheck={false}
					/>
				</Form.Item>
				{errorKey ? <Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 16 }} /> : null}
				<Button type="primary" htmlType="submit" loading={busy}>
					{t('bootstrap.action')}
				</Button>
			</Form>
		</Card>
	);
}
