/**
 * LoginPage: passkey sign-in is the routine entry; the CLI one-time code is
 * the identity-proofing and recovery entry. Before platform initialization
 * the page shows the exact CLI steps AND the code form, so the init code can
 * be redeemed right here. A code redemption for an account without a
 * credential lands on the binding page instead of the console.
 */
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Divider, Form, Input, Typography } from 'antd';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import {
	sessionSnapshot,
	signInWithCode,
	signInWithPasskey,
	subscribeSession
} from '../lib/session';
import { WebAuthnError } from '../lib/webauthn';

interface CodeForm {
	code: string;
}

function webauthnErrorKey(err: unknown): MessageKey {
	if (err instanceof WebAuthnError) {
		switch (err.kind) {
			case 'cancelled':
				return 'webauthn.cancelled';
			case 'duplicate':
				return 'webauthn.duplicate';
			case 'rp-mismatch':
				return 'webauthn.rpMismatch';
			case 'insecure-context':
				return 'webauthn.insecureContext';
			case 'unsupported':
				return 'webauthn.unsupported';
			default:
				return 'webauthn.unknown';
		}
	}
	return apiErrorKey(err) ?? 'error.generic';
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

function CodeRedeemForm({ onDone }: { onDone: (status: string) => void }) {
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);

	async function handleExchange(values: CodeForm) {
		setBusy(true);
		setErrorKey(null);
		try {
			onDone(await signInWithCode(values.code.trim()));
		} catch (err) {
			setErrorKey(apiErrorKey(err) ?? 'error.generic');
		} finally {
			setBusy(false);
		}
	}

	return (
		<Form<CodeForm> layout="vertical" onFinish={handleExchange} disabled={busy}>
			{errorKey ? (
				<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 16 }} />
			) : null}
			<Form.Item
				name="code"
				label={session.platformInitialized ? t('login.recoveryLabel') : t('login.codeLabel')}
				rules={[{ required: true, message: t('login.codeRequired') }]}
			>
				<Input autoComplete="one-time-code" placeholder="XXXX-XXXX-XXXX-XXXX" allowClear />
			</Form.Item>
			<Button type={session.platformInitialized ? 'default' : 'primary'} htmlType="submit" block loading={busy}>
				{t('login.action')}
			</Button>
			<Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
				{t('login.codeHint')}
			</Typography.Paragraph>
		</Form>
	);
}

export default function LoginPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	function handleDone(status: string) {
		navigate(status === 'restricted' ? '/register-passkey' : '/sites', { replace: true });
	}

	async function handlePasskey() {
		setBusy(true);
		setErrorKey(null);
		try {
			await signInWithPasskey();
			navigate('/sites', { replace: true });
		} catch (err) {
			setErrorKey(webauthnErrorKey(err));
		} finally {
			setBusy(false);
		}
	}

	if (!session.platformInitialized) {
		return (
			<div className="auth-screen">
				<div className="auth-stack">
					<InitGuide />
					<Card title={t('login.title')} className="auth-card">
						<CodeRedeemForm onDone={handleDone} />
					</Card>
				</div>
			</div>
		);
	}

	return (
		<div className="auth-screen">
			<Card title={t('login.title')} className="auth-card">
				{errorKey ? (
					<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 16 }} />
				) : null}
				<Button
					type="primary"
					block
					loading={busy}
					disabled={!session.passkeyAvailable}
					onClick={() => void handlePasskey()}
				>
					{t('login.passkeyAction')}
				</Button>
				<Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
					{session.passkeyAvailable ? t('login.passkeyHint') : t('login.passkeyUnavailable')}
				</Typography.Paragraph>
				<Divider style={{ margin: '16px 0' }} />
				<CodeRedeemForm onDone={handleDone} />
			</Card>
		</div>
	);
}
