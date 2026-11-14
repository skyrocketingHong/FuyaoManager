import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import { Alert, App, Button, Select, Space } from 'antd';
import { useParams } from 'react-router-dom';
import { useCurrentSite } from '../../components/layout/SiteBoundary';
import { usePageHeader } from '../../components/PageHeader';
import { Panel } from '../../components/Panel';
import PageLoading from '../../components/PageLoading';
import { useAdminMutation, useAdminQuery } from '../../lib/admin/requests';
import {
	getContentSources,
	putContentSources,
	contentResources,
	type ContentSources
} from '../../lib/api/content';
import { ApiError } from '../../lib/api/client';
import { EditableDoc } from '../../lib/editing/EditableDoc';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { t, type MessageKey } from '../../lib/i18n';

export default function ContentSourcesPage() {
	const { siteId = '' } = useParams();
	return <ContentSourcesEditor key={siteId} siteId={siteId} />;
}
function ContentSourcesEditor({ siteId }: { siteId: string }) {
	const site = useCurrentSite();
	const { modal } = App.useApp();
	const editor = useMemo(() => new EditableDoc<ContentSources>(), []);
	const state = useSyncExternalStore(editor.subscribe, editor.getSnapshot);
	const revision = useRef(0);
	const load = useCallback((signal: AbortSignal) => getContentSources(siteId, signal), [siteId]);
	const query = useAdminQuery(load);
	const mutation = useAdminMutation();
	useEffect(() => {
		if (query.data?.etag) {
			revision.current = query.data.data.revision;
			editor.load(query.data.data.sources, query.data.etag);
		}
	}, [query.data, editor]);
	const dirty = editor.isDirty();
	const editable = site?.role === 'owner' && !query.loading && !query.error;
	const values = state.draft ?? state.baseline;
	useUnsavedGuard(dirty || state.saving);
	function refresh() {
		const run = () => {
			mutation.clearError();
			query.reload();
		};
		if (dirty)
			modal.confirm({
				title: t('draft.leaveTitle'),
				content: t('draft.leaveBody'),
				okText: t('draft.leaveConfirm'),
				cancelText: t('draft.leaveCancel'),
				onOk: run
			});
		else run();
	}
	async function save() {
		if (!editable || mutation.busy) return;
		const snapshot = editor.beginSave();
		if (!snapshot) return;
		await mutation.run(
			async () => {
				try {
					return await putContentSources(
						siteId,
						{ sources: snapshot, revision: revision.current },
						state.etag
					);
				} catch (error) {
					editor.rejectSave(error instanceof ApiError && error.status === 412);
					throw error;
				}
			},
			(saved) => {
				if (!saved.etag) throw new Error('Missing content sources ETag');
				revision.current = saved.data.revision;
				editor.resolveSave(saved.data.sources, saved.etag);
			}
		);
	}
	async function syncConflict() {
		await mutation.run(
			() => getContentSources(siteId),
			(response) => {
				if (!response.etag) throw new Error('Missing content sources ETag');
				revision.current = response.data.revision;
				const merged = { ...response.data.sources };
				if (state.draft && state.baseline)
					for (const resource of contentResources) {
						if (state.draft[resource] !== state.baseline[resource])
							merged[resource] = state.draft[resource];
					}
				editor.syncAfterConflict(response.data.sources, response.etag);
				editor.setDraft(merged);
			}
		);
	}
	const actions = (
		<Space size={8}>
			<Button disabled={mutation.busy || query.loading} onClick={refresh}>
				{t('admin.refresh')}
			</Button>
			<Button
				type="primary"
				disabled={!editable || !dirty}
				loading={mutation.busy}
				onClick={() => void save()}
			>
				{t('common.save')}
			</Button>
		</Space>
	);
	const header = usePageHeader({ actions });
	return (
		<div className="page-stack">
			{!header && <div className="table-panel-header">{actions}</div>}
			{query.error && (
				<Alert
					type="error"
					title={t(query.error)}
					showIcon
					action={<Button onClick={refresh}>{t('common.retry')}</Button>}
				/>
			)}
			{mutation.error && !state.conflict && (
				<Alert type="error" title={t(mutation.error)} showIcon />
			)}
			{state.conflict && (
				<Alert
					type="warning"
					title={t('posts.conflictTitle')}
					showIcon
					action={
						<Space>
							<Button disabled={mutation.busy} onClick={() => void syncConflict()}>
								{t('posts.conflictAction')}
							</Button>
							<Button danger disabled={mutation.busy} onClick={() => editor.discardLocal()}>
								{t('posts.discardLocal')}
							</Button>
						</Space>
					}
				/>
			)}
			<Panel title={t('nav.contentSources')}>
				{!values ? (
					query.loading ? (
						<PageLoading />
					) : null
				) : (
					<div className="form-grid">
						{contentResources.map((resource) => (
							<label key={resource} className="field">
								<span className="field-label">{t(`nav.${resource}` as MessageKey)}</span>
								<Select
									aria-label={t(`nav.${resource}` as MessageKey)}
									value={values[resource]}
									disabled={!editable || mutation.busy}
									options={[
										{ value: 'platform', label: t('content.platform') },
										{ value: 'independent', label: t('content.independent') }
									]}
									onChange={(value) =>
										editor.mutate((current) => ({ ...current, [resource]: value }))
									}
								/>
							</label>
						))}
					</div>
				)}
			</Panel>
			{/* Saving only switches the source mapping; publishing stays a
				separate release action. Stated once, without alert weight. */}
			<p className="subtle">{t('content.sourcesSavedSeparately')}</p>
		</div>
	);
}
