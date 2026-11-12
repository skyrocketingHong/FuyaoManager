/**
 * AccountSettingsPage: self-service account rename. Shows the current
 * username, edits it with draft protection, and applies the change through
 * PATCH /api/v1/account/profile — a failure keeps the draft in the form; a
 * success refreshes the session store so the top bar follows. Every async
 * result is guarded by the account's stable id and the account generation:
 * a rename response for account A can never touch the page or the session
 * after a switch to account B.
 */
import { useEffect, useRef, useState } from 'react';
import { useSyncExternalStore } from 'react';
import { Button, Form, Input, Typography, message } from 'antd';
import Trans from '../components/i18n/Trans';
import { usePageHeader } from '../components/PageHeader';
import PageLoading from '../components/PageLoading';
import { Panel } from '../components/Panel';
import { updateAccountProfile } from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { t } from '../lib/i18n';
import { renameCurrentUser, sessionSnapshot, subscribeSession } from '../lib/session';
import { useUnsavedGuard } from '../lib/editing/useUnsavedGuard';

export default function AccountSettingsPage() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [messageApi, contextHolder] = message.useMessage();
	const [saving, setSaving] = useState(false);
	const [username, setUsername] = useState<string | null>(null);
	// The latest logged-in account id, readable from async handlers.
	const userIdRef = useRef<string | null>(null);
	// Account generation: bumped whenever the logged-in account changes and
	// on unmount — a rename response applies only while it still matches.
	const generation = useRef(0);

	const user = session.status === 'authenticated' ? session.user : null;
	const draft = username ?? user?.username ?? '';
	const dirty = user !== null && draft !== user.username;
	useUnsavedGuard(dirty);

	useEffect(() => {
		// Switching to another account (or losing the session) invalidates
		// everything in flight and resets the draft to the new account's
		// stored username.
		const id = user?.id ?? null;
		if (id !== userIdRef.current) {
			userIdRef.current = id;
			generation.current++;
			setUsername(user?.username ?? null);
			setSaving(false);
		}
	}, [user]);

	async function handleSave() {
		if (saving || user === null) return;
		const targetId = user.id;
		const myGen = generation.current;
		setSaving(true);
		try {
			const updated = await updateAccountProfile(draft);
			if (generation.current !== myGen || userIdRef.current !== targetId) return;
			// The store double-checks the stable id on its side.
			renameCurrentUser(targetId, updated.username);
			setUsername(updated.username);
			void messageApi.success(t('account.saved'));
		} catch (err) {
			if (generation.current !== myGen || userIdRef.current !== targetId) return;
			// The failure keeps the draft in the form for another attempt.
			const key = apiErrorKey(err);
			void messageApi.warning(key !== null ? t(key) : t('error.generic'));
		} finally {
			setSaving(false);
		}
	}

	const headerActions = (
		<Button type="primary" loading={saving} disabled={!dirty} onClick={() => void handleSave()}>
			<Trans text={t('common.save')} />
		</Button>
	);
	const headerActive = usePageHeader({ actions: headerActions });

	if (user === null) {
		return (
			<div className="page-stack">
				{contextHolder}
				<PageLoading spec="page" label={t('common.loading')} />
			</div>
		);
	}

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{headerActions}</div>
			) : null}
			<Panel title={t('account.title')}>
				<Form layout="vertical">
					<Form.Item label={t('account.usernameLabel')} tooltip={t('account.usernameHint')}>
						<Input
							value={draft}
							maxLength={64}
							onChange={(event) => setUsername(event.target.value)}
							aria-label={t('account.usernameLabel')}
						/>
					</Form.Item>
					<Typography.Text type="secondary" style={{ fontSize: 12 }}>
						{t('account.usernameHint')}
					</Typography.Text>
				</Form>
			</Panel>
		</div>
	);
}
