import { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Form, Input, Select, Space } from 'antd';
import SiteNav from '../../components/sites/SiteNav';
import Trans from '../../components/i18n/Trans';
import { SettingsEditor } from '../../lib/sites/settings-editor';
import { t } from '../../lib/i18n';

export default function SettingsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	// A fresh editor per siteId keeps baseline, ETag, and edits from leaking
	// across sites.
	const editor = useMemo(
		() =>
			new SettingsEditor({
				onUnauthenticated: async () => {
					navigate('/login');
				}
			}),
		// eslint-disable-next-line react-hooks/exhaustive-deps
		[siteId]
	);

	useEffect(() => {
		void editor.load(siteId ?? '');
	}, [editor, siteId]);

	const state = useSyncExternalStore(editor.subscribe, editor.getSnapshot);
	const phase = state.phase;
	const dirty = editor.dirtyFields();
	const editable = editor.editable();

	return (
		<>
			<SiteNav active="settings" />
			<Card title={<Trans text={t('settings.title')} />} style={{ marginTop: 16 }}>
				{phase.kind === 'loading' ? (
					<p role="status">{t('common.loading')}</p>
				) : state.site === null ? (
					// Load failed before the site was known: nothing to edit yet.
					phase.kind === 'error' ? (
						<Alert type="error" showIcon message={t(phase.key as never)} />
					) : null
				) : (
					<>
						{phase.kind === 'saved' && dirty.length === 0 ? (
							<Alert
								type="success"
								showIcon
								message={t('settings.savedAt', { revision: phase.revision })}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{phase.kind === 'saved' && dirty.length > 0 ? (
							<Alert
								type="success"
								showIcon
								message={t('settings.savedWithEdits', { revision: phase.revision })}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{phase.kind === 'error' ? (
							// The form stays below, editable, so the user can adjust and retry.
							<Alert type="error" showIcon message={t(phase.key as never)} style={{ marginBottom: 16 }} />
						) : null}
						{phase.kind === 'conflict' ? (
							<Alert
								type="warning"
								showIcon
								message={<Trans text={t('settings.conflictTitle')} />}
								description={
									<>
										{t('settings.conflictBody')}
										{phase.synced ? (
											<div style={{ marginTop: 8 }}>{t('settings.conflictSynced')}</div>
										) : (
											<div style={{ marginTop: 8 }}>
												<Button onClick={() => void editor.syncAfterConflict()}>
													<Trans text={t('settings.conflictAction')} />
												</Button>
											</div>
										)}
									</>
								}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{!editable ? (
							<Alert type="info" showIcon message={t('settings.viewerNotice')} style={{ marginBottom: 16 }} />
						) : null}
						<Form labelCol={{ span: 4 }}>
							<Form.Item label={t('settings.name')} htmlFor="settings-name">
								<Input
									id="settings-name"
									value={state.name}
									onChange={(e) => editor.setField('name', e.target.value)}
									disabled={!editable}
								/>
							</Form.Item>
							<Form.Item label={t('settings.description')} htmlFor="settings-description">
								<Input.TextArea
									id="settings-description"
									value={state.description}
									onChange={(e) => editor.setField('description', e.target.value)}
									disabled={!editable}
									rows={2}
								/>
							</Form.Item>
							<Form.Item label={t('settings.defaultLocale')} htmlFor="settings-locale">
								<Select
									id="settings-locale"
									value={state.defaultLocale}
									onChange={(value) => editor.setField('defaultLocale', value)}
									disabled={!editable}
									options={[
										{ value: 'zh-CN', label: t('settings.locale.zh-CN') },
										{ value: 'en-US', label: t('settings.locale.en-US') }
									]}
								/>
							</Form.Item>
						</Form>
						<Space>
							<Button
								type="primary"
								loading={phase.kind === 'saving'}
								disabled={!editable || dirty.length === 0}
								onClick={() => void editor.save()}
							>
								<Trans text={t('common.save')} />
							</Button>
						</Space>
					</>
				)}
			</Card>
		</>
	);
}
