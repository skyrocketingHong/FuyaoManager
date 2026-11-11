import { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Card, Form, Input, Radio, Space, Tag, Typography } from 'antd';
import SiteNav from '../../components/sites/SiteNav';
import { ThemeSelection } from '../../lib/themes/theme-selection';
import { t } from '../../lib/i18n';

export default function ThemePage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const store = useMemo(
		() =>
			new ThemeSelection({
				onUnauthenticated: async () => {
					navigate('/login');
				}
			}),
		// eslint-disable-next-line react-hooks/exhaustive-deps
		[siteId]
	);

	useEffect(() => {
		void store.load(siteId ?? '');
	}, [store, siteId]);

	const state = useSyncExternalStore(store.subscribe, store.getSnapshot);
	const phase = state.phase;
	const editable = store.editable();

	return (
		<>
			<SiteNav active="theme" />
			<Card title={t('themes.title')} style={{ marginTop: 16 }}>
				{phase.kind === 'loading' ? (
					<p role="status">{t('common.loading')}</p>
				) : phase.kind === 'error' ? (
					<Alert type="error" showIcon message={t(phase.key as never)} />
				) : (
					<>
						{phase.kind === 'saved' ? (
							<Alert type="success" showIcon message={t('common.saved')} style={{ marginBottom: 16 }} />
						) : null}
						{phase.kind === 'conflict' ? (
							<Alert
								type="warning"
								showIcon
								message={t('settings.conflictTitle')}
								description={
									phase.synced ? (
										t('settings.conflictSynced')
									) : (
										<Button onClick={() => void store.syncAfterConflict(siteId ?? '')}>
											{t('settings.conflictAction')}
										</Button>
									)
								}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						<Form labelCol={{ span: 4 }}>
							<Form.Item label={t('themes.current')}>
								{state.hasSelection && state.selected ? (
									<Tag color="green">
										{state.selected.replace('@', ' @ ')}
									</Tag>
								) : (
									<Tag>{t('themes.unselected')}</Tag>
								)}
							</Form.Item>
							<Form.Item label={t('themes.select')}>
								<Radio.Group
									value={state.selected}
									onChange={(e) => store.select(e.target.value as string)}
									disabled={!editable}
								>
									<Space direction="vertical">
										{state.themes.map((theme) => (
											<Radio key={`${theme.themeId}@${theme.version}`} value={`${theme.themeId}@${theme.version}`}>
												{theme.name ?? theme.themeId}
												<Typography.Text type="secondary"> {theme.themeId} @ {theme.version}</Typography.Text>
											</Radio>
										))}
									</Space>
								</Radio.Group>
							</Form.Item>
							<Form.Item label={t('themes.config')}>
								<Input.TextArea
									value={state.configText}
									onChange={(e) => store.setConfig(e.target.value)}
									disabled={!editable}
									rows={6}
									spellCheck={false}
									style={{ fontFamily: 'monospace' }}
								/>
							</Form.Item>
						</Form>
						<Button
							type="primary"
							loading={phase.kind === 'saving'}
							disabled={!editable || state.selected === null}
							onClick={() => void store.save()}
						>
							{t('themes.save')}
						</Button>
					</>
				)}
			</Card>
		</>
	);
}
