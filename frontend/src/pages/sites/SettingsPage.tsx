import { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Form, Input, Select, Space, Typography } from 'antd';
import SiteNav from '../../components/sites/SiteNav';
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
			<Card title={t('settings.title')} style={{ marginTop: 16 }}>
				{phase.kind === 'loading' ? (
					<p role="status">{t('common.loading')}</p>
				) : phase.kind === 'error' ? (
					<Alert type="error" showIcon message={t(phase.key as never)} />
				) : state.site === null ? null : (
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
						{phase.kind === 'conflict' ? (
							<Alert
								type="warning"
								showIcon
								message={t('settings.conflictTitle')}
								description={
									<>
										<Typography.Paragraph>{t('settings.conflictBody')}</Typography.Paragraph>
										{phase.synced ? (
											<Typography.Paragraph>{t('settings.conflictSynced')}</Typography.Paragraph>
										) : (
											<Button onClick={() => void editor.syncAfterConflict()}>
												{t('settings.conflictAction')}
											</Button>
										)}
									</>
								}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{!editable ? <Alert type="info" showIcon message={t('settings.viewerNotice')} style={{ marginBottom: 16 }} /> : null}
						<Form labelCol={{ span: 4 }} disabled={!editable}>
							<Form.Item label={t('settings.name')}>
								<Input
									value={state.name}
									onChange={(e) => editor.setField('name', e.target.value)}
									disabled={!editable}
								/>
							</Form.Item>
							<Form.Item label={t('settings.description')}>
								<Input.TextArea
									value={state.description}
									onChange={(e) => editor.setField('description', e.target.value)}
									disabled={!editable}
									rows={2}
								/>
							</Form.Item>
							<Form.Item label={t('settings.defaultLocale')}>
								<Select
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
								{t('common.save')}
							</Button>
						</Space>
					</>
				)}
			</Card>
		</>
	);
}
