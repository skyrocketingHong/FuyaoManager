import { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import {
	Alert,
	Button,
	Form,
	Input,
	InputNumber,
	Radio,
	Select,
	Space,
	Switch,
	Typography
} from 'antd';
import Trans from '../../components/i18n/Trans';
import PageLoading from '../../components/PageLoading';
import StatusTag from '../../components/StatusTag';
import { Panel } from '../../components/Panel';
import { ThemeSelection } from '../../lib/themes/theme-selection';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { configTextFromFields, fieldsFromSchema } from '../../lib/themes/schemaForm';
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
	useUnsavedGuard(editable && store.dirty());

	// Visual fields for the selected theme's declared config schema. The
	// JSON view and the form edit the same store state (configText).
	const selectedTheme = state.themes.find((th) => `${th.themeId}@${th.version}` === state.selected);
	const schemaFields = fieldsFromSchema(selectedTheme?.configSchema);
	const configValues = (() => {
		try {
			return JSON.parse(state.configText || '{}') as Record<string, unknown>;
		} catch {
			return {};
		}
	})();
	function setFieldValue(key: string, value: unknown) {
		const next = configTextFromFields(state.configText, schemaFields, {
			...configValues,
			[key]: value
		});
		if (next !== null) store.setConfig(next);
	}

	return (
		<div className="page-stack">
			<Panel title={<Trans text={t('themes.title')} />}>
				{phase.kind === 'loading' ? (
					<PageLoading spec="section" label={t('common.loading')} />
				) : (
					<>
						{phase.kind === 'saved' ? (
							<Alert
								type="success"
								showIcon
								title={<Trans text={t('common.saved')} />}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{phase.kind === 'error' ? (
							<Alert
								type="error"
								showIcon
								title={t(phase.key as never)}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{phase.kind === 'conflict' ? (
							<Alert
								type="warning"
								showIcon
								title={<Trans text={t('settings.conflictTitle')} />}
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
							<Form.Item label={t('themes.current')} htmlFor="theme-saved">
								<span id="theme-saved">
									{state.hasSelection && state.savedSelected ? (
										<StatusTag kind="success">
											{state.savedSelected.replace('@', ' @ ')}
										</StatusTag>
									) : (
										<StatusTag kind="neutral">{t('themes.unselected')}</StatusTag>
									)}
								</span>
							</Form.Item>
							<Form.Item label={t('themes.select')} htmlFor="theme-select">
								<Radio.Group
									id="theme-select"
									value={state.selected}
									onChange={(e) => store.select(e.target.value as string)}
									disabled={!editable}
								>
									<Space orientation="vertical">
										{state.themes.map((theme) => (
											<Radio
												key={`${theme.themeId}@${theme.version}`}
												value={`${theme.themeId}@${theme.version}`}
											>
												{theme.name ?? theme.themeId}
												<Typography.Text type="secondary">
													{' '}
													{theme.themeId} @ {theme.version}
												</Typography.Text>
											</Radio>
										))}
									</Space>
								</Radio.Group>
							</Form.Item>
							{schemaFields.length > 0 ? (
								schemaFields.map((field) => (
									<Form.Item
										key={field.key}
										label={field.label}
										htmlFor={`theme-field-${field.key}`}
									>
										{field.type === 'boolean' ? (
											<Switch
												id={`theme-field-${field.key}`}
												checked={Boolean(configValues[field.key])}
												disabled={!editable}
												onChange={(checked) => setFieldValue(field.key, checked)}
											/>
										) : field.type === 'enum' ? (
											<Select
												id={`theme-field-${field.key}`}
												value={configValues[field.key] as string | undefined}
												disabled={!editable}
												options={(field.options ?? []).map((opt) => ({ value: opt, label: opt }))}
												onChange={(value) => setFieldValue(field.key, value)}
												allowClear
											/>
										) : field.type === 'number' ? (
											<InputNumber
												id={`theme-field-${field.key}`}
												value={configValues[field.key] as number | undefined}
												disabled={!editable}
												onChange={(value) => setFieldValue(field.key, value)}
											/>
										) : (
											<Input
												id={`theme-field-${field.key}`}
												value={configValues[field.key] as string | undefined}
												disabled={!editable}
												placeholder={field.placeholder}
												onChange={(e) => setFieldValue(field.key, e.target.value)}
											/>
										)}
									</Form.Item>
								))
							) : (
								<Typography.Text type="secondary">{t('themes.noSchemaFields')}</Typography.Text>
							)}
							<Form.Item label={t('themes.configJson')} htmlFor="theme-config">
								<Input.TextArea
									id="theme-config"
									value={state.configText}
									onChange={(e) => store.setConfig(e.target.value)}
									disabled={!editable}
									rows={6}
									spellCheck={false}
									style={{ fontFamily: 'monospace' }}
									aria-label={t('themes.configJson')}
								/>
							</Form.Item>
						</Form>
						<Button
							type="primary"
							loading={phase.kind === 'saving'}
							disabled={!editable || state.selected === null}
							onClick={() => void store.save()}
						>
							<Trans text={t('themes.save')} />
						</Button>
					</>
				)}
			</Panel>
		</div>
	);
}
