/**
 * BindPasskeyPage: the only page a restricted registration session can use.
 * The account identity came from a CLI one-time code; until a passkey is
 * bound for the current relying party the business surfaces stay closed.
 * Cancelling the authenticator keeps the session alive — the user retries
 * here without redeeming another code.
 */
import { useState } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Typography } from 'antd';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { bindPasskey, sessionSnapshot, signOut, subscribeSession } from '../lib/session';
import { detectCapability, WebAuthnError } from '../lib/webauthn';

function ceremonyErrorKey(err: unknown): MessageKey {
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

export default function BindPasskeyPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [busy, setBusy] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	if (session.status === 'authenticated') return <Navigate to="/sites" replace />;
	if (session.status === 'anonymous') return <Navigate to="/login" replace />;

	const capability = detectCapability();
	const usable = capability === 'available' && session.passkeyAvailable;

	async function handleBind() {
		setBusy(true);
		setErrorKey(null);
		try {
			await bindPasskey();
			navigate('/sites', { replace: true });
		} catch (err) {
			// Stay on the page with a retry entry; never loop the authenticator
			// prompt automatically.
			setErrorKey(ceremonyErrorKey(err));
		} finally {
			setBusy(false);
		}
	}

	return (
		<div className="auth-screen">
			<Card title={t('bind.title')} className="auth-card">
				<Typography.Paragraph>
					{t('bind.description', { username: session.user?.username ?? '' })}
				</Typography.Paragraph>
				{!usable ? (
					<Alert
						type="warning"
						showIcon
						message={t(capability === 'available' ? 'error.passkeyUnavailable' : 'bind.unavailableHint')}
						style={{ marginBottom: 16 }}
					/>
				) : null}
				{errorKey ? (
					<Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 16 }} />
				) : null}
				<Button
					type="primary"
					block
					loading={busy}
					disabled={!usable}
					onClick={() => void handleBind()}
				>
					{errorKey ? t('bind.retry') : t('bind.action')}
				</Button>
				<Button
					type="text"
					block
					style={{ marginTop: 8 }}
					onClick={() => {
						void signOut().then(() => navigate('/login', { replace: true }));
					}}
				>
					{t('bind.signOut')}
				</Button>
			</Card>
		</div>
	);
}
