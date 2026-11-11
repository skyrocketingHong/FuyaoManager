/**
 * LoginPage: redeems a server-CLI one-time code (initialization or per-login)
 * for a session. When the platform is not initialized yet, the page shows the
 * exact server CLI steps instead of a dead form.
 */
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Form, Input, Typography } from 'antd';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, signInWithCode, subscribeSession } from '../lib/session';

interface CodeForm {
	code: string;
}

function InitGuide() {
	return (
		<Card title={t('login.initTitle')} className="auth-card">
			<Typography.Paragraph>{t('login.initDescription')}</Typography.Paragraph>
			<Typography.Paragraph>
				<Typography.Text strong>1. </Typography.Text>
				{t('login.initStep1')}
			</Typography.Paragraph>
			<Typography.Paragraph className="code-hint">
				fuyao-admin init-admin --username &lt;name&gt;
			</Typography.Paragraph>
			<Typography.Paragraph>
				<Typography.Text strong>2. </Typography.Text>
				{t('login.initStep2')}
			</Typography.Paragraph>
			<Typography.Paragraph style={{ marginBottom: 0 }}>
				<Typography.Text strong>3. </Typography.Text>
				{t('login.initStep3')}
			</Typography.Paragraph>
		</Card>
	);
}

export default function LoginPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	if (!session.platformInitialized && session.status !== 'authenticated') {
		return (
			<div className="auth-screen">
				<InitGuide />
			</div>
		);
	}

	async function handleExchange(values: CodeForm) {
		setBusy(true);
		setErrorKey(null);
		try {
			await signInWithCode(values.code.trim());
			navigate('/sites');
		} catch (err) {
			setErrorKey(apiErrorKey(err));
		} finally {
			setBusy(false);
		}
	}

	return (
		<div className="auth-screen">
			<Card title={t('login.title')} className="auth-card">
				<Typography.Paragraph>{t('login.description')}</Typography.Paragraph>
				{errorKey ? (
					<Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 16 }} />
				) : null}
				<Form<CodeForm> layout="vertical" onFinish={handleExchange} disabled={busy}>
					<Form.Item
						name="code"
						label={t('login.codeLabel')}
						rules={[{ required: true, message: t('login.codeRequired') }]}
					>
						<Input autoComplete="one-time-code" placeholder="XXXX-XXXX-XXXX-XXXX" allowClear />
					</Form.Item>
					<Button type="primary" htmlType="submit" block loading={busy}>
						{t('login.action')}
					</Button>
				</Form>
				<Typography.Paragraph type="secondary" style={{ marginTop: 16, marginBottom: 0 }}>
					{t('login.codeHint')}
				</Typography.Paragraph>
			</Card>
		</div>
	);
}
