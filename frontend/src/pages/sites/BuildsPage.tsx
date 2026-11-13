import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Popconfirm, Space, Typography, message } from 'antd';
import DataTable from '../../components/DataTable';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import StatusTag from '../../components/StatusTag';
import { TablePanel } from '../../components/Panel';
import {
	canEditSite,
	closePreviewLink,
	createBuild,
	createPreviewLink,
	getSite,
	listBuilds,
	type BuildTask,
	type Site
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

const terminalStatus = new Set(['succeeded', 'failed']);

export default function BuildsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [builds, setBuilds] = useState<BuildTask[] | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [creating, setCreating] = useState(false);
	const [closing, setClosing] = useState<string | null>(null);
	const [opening, setOpening] = useState<string | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	// Request epoch: every refresh bumps it; a response applies only while it
	// is still the newest. Route changes and unmount bump it too, so a late
	// answer for a previous site (or after leaving the page) can never
	// overwrite the current list.
	const epoch = useRef(0);
	// Mount/site generation: bumped on every site switch and on unmount. Async
	// handlers (open, close, create) capture the generation at click time and
	// apply their results only while it still matches — a late success from a
	// previous site must not refresh with its captured old siteId, yet it
	// still closes exactly the window it owns.
	const generation = useRef(0);
	// Opened preview windows keyed by "siteId/buildId". The server-side close
	// closes exactly its own window; entries for manually closed windows are
	// dropped lazily. Windows survive site switches — they are independent
	// origins the user may still be reading.
	const previewWindows = useRef(new Map<string, Window>());
	const previewRevisions = useRef(new Map<string, number>());
	const openingPreviews = useRef(new Set<string>());
	const closingPreviews = useRef(new Set<string>());

	const refresh = useCallback(async () => {
		if (!siteId) return;
		const myGen = generation.current;
		const my = ++epoch.current;
		try {
			const list = await listBuilds(siteId);
			if (my !== epoch.current || myGen !== generation.current) return;
			setBuilds(list);
		} catch (err) {
			if (my !== epoch.current || myGen !== generation.current) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		}
	}, [siteId, navigate]);

	// Site change (same component instance, different route param): drop all
	// previous data first so nothing from the old site survives, then load.
	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		generation.current++;
		setSite(null);
		setBuilds(null);
		setErrorKey(null);
		// In-flight action spinners belong to the previous site's rows.
		setCreating(false);
		setOpening(null);
		setClosing(null);
		let cancelled = false;
		getSite(siteId)
			.then((s) => {
				if (!cancelled) setSite(s);
			})
			.catch((err) => {
				if (cancelled) return;
				if (apiErrorKey(err) === 'error.unauthenticated') {
					navigate('/login');
				}
			});
		void refresh();
		return () => {
			// Leaving the page or switching sites invalidates everything
			// in flight, including the site record.
			epoch.current++;
			generation.current++;
			cancelled = true;
		};
	}, [siteId, refresh, navigate]);

	// Poll while any task is queued or running, or while a preview origin is
	// live: previewActive is runtime state that must keep tracking the
	// server. Cleared on unmount and site switch via the effect cleanup above
	// (refresh identity changes with siteId, re-running this effect).
	useEffect(() => {
		if (builds === null || !builds.some((b) => !terminalStatus.has(b.status) || b.previewActive))
			return;
		const timer = window.setInterval(() => void refresh(), 3000);
		return () => window.clearInterval(timer);
	}, [builds, refresh]);

	async function handleCreate() {
		if (!siteId) return;
		const myGen = generation.current;
		setCreating(true);
		try {
			await createBuild(siteId);
			if (generation.current !== myGen) return;
			await refresh();
		} catch (err) {
			if (generation.current !== myGen) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		} finally {
			setCreating(false);
		}
	}

	async function handlePreview(buildID: string, active: boolean) {
		if (!siteId) return;
		const myGen = generation.current;
		// A live preview reuses its tracked window instead of issuing again.
		const windowKey = `${siteId}/${buildID}`;
		if (openingPreviews.current.has(windowKey) || closingPreviews.current.has(windowKey)) return;
		const revision = previewRevisions.current.get(windowKey) ?? 0;
		const existing = previewWindows.current.get(windowKey);
		if (active && existing && !existing.closed) {
			existing.focus();
			return;
		}
		setOpening(buildID);
		openingPreviews.current.add(windowKey);
		// The blank window must be opened synchronously inside the click:
		// after an await the user gesture is gone and the browser blocks the
		// popup. The opener is severed before the preview navigation.
		const win = window.open('', '_blank');
		if (!win) {
			openingPreviews.current.delete(windowKey);
			setOpening(null);
			void messageApi.warning(t('builds.previewBlocked'));
			return;
		}
		win.opener = null;
		previewWindows.current.set(windowKey, win);
		try {
			const link = await createPreviewLink(siteId, buildID);
			if (
				generation.current !== myGen ||
				revision !== (previewRevisions.current.get(windowKey) ?? 0)
			) {
				// The user switched sites (or left the page) mid-flight: the
				// response belongs to the previous site, so only the blank
				// window this request opened is discarded.
				win.close();
				if (previewWindows.current.get(windowKey) === win) previewWindows.current.delete(windowKey);
				return;
			}
			win.location.href = link.url;
			await refresh();
		} catch (err) {
			win.close();
			if (previewWindows.current.get(windowKey) === win) previewWindows.current.delete(windowKey);
			if (
				generation.current !== myGen ||
				revision !== (previewRevisions.current.get(windowKey) ?? 0)
			)
				return;
			// Unknown failures still surface: silence would leave a blank
			// window closing with no explanation.
			void messageApi.warning(t(apiErrorKey(err) ?? 'error.generic'));
		} finally {
			openingPreviews.current.delete(windowKey);
			if (generation.current === myGen)
				setOpening((current) => (current === buildID ? null : current));
		}
	}

	// Closing is a server-side action: tokens are revoked and the listener and
	// port are released. It is confirmed, idempotent, and scoped to this one
	// build's preview — success closes exactly that build's window (keyed by
	// the captured site and build, even when the site switched mid-flight),
	// while state updates and feedback stay generation-guarded; failure keeps
	// the window and the live state untouched.
	async function handleClosePreview(buildID: string) {
		if (!siteId) return;
		const myGen = generation.current;
		const windowKey = `${siteId}/${buildID}`;
		if (closingPreviews.current.has(windowKey)) return;
		closingPreviews.current.add(windowKey);
		previewRevisions.current.set(windowKey, (previewRevisions.current.get(windowKey) ?? 0) + 1);
		setClosing(buildID);
		try {
			await closePreviewLink(siteId, buildID);
			const win = previewWindows.current.get(windowKey);
			if (win && !win.closed) win.close();
			previewWindows.current.delete(windowKey);
			if (generation.current !== myGen) return;
			void messageApi.success(t('builds.previewClosed'));
			await refresh();
		} catch (err) {
			if (generation.current !== myGen) return;
			// The window and the live state stay untouched; the failure is
			// always reported, even for unmapped error codes.
			void messageApi.warning(t(apiErrorKey(err) ?? 'error.generic'));
		} finally {
			closingPreviews.current.delete(windowKey);
			if (generation.current === myGen)
				setClosing((current) => (current === buildID ? null : current));
		}
	}

	const editable = site !== null && canEditSite(site);

	const createButton = (
		<Popconfirm
			title={t('builds.create')}
			onConfirm={() => void handleCreate()}
			disabled={!editable}
		>
			<Button type="primary" loading={creating} disabled={!editable}>
				<Trans text={t('builds.create')} />
			</Button>
		</Popconfirm>
	);
	const headerActive = usePageHeader({ actions: createButton });

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{createButton}</div>
			) : null}
			{errorKey !== null ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey ?? 'error.generic')}
					style={{ marginBottom: 16 }}
				/>
			) : null}
			{builds === null ? (
				<PageLoading spec="page" label={t('common.loading')} />
			) : builds.length === 0 ? (
				<TablePanel>
					<p>{t('builds.empty')}</p>
				</TablePanel>
			) : (
				<TablePanel>
					<DataTable<BuildTask>
						rowKey="id"
						dataSource={builds}
						pagination={false}
						columns={[
							{
								title: 'ID',
								dataIndex: 'id',
								render: (id: string) => (
									<Typography.Text copyable code>
										{id.slice(0, 8)}
									</Typography.Text>
								)
							},
							{
								title: t('builds.status'),
								dataIndex: 'status',
								render: (status: string) => (
									<StatusTag
										kind={
											status === 'succeeded'
												? 'success'
												: status === 'failed'
													? 'danger'
													: status === 'running'
														? 'info'
														: 'neutral'
										}
									>
										{t(`builds.status.${status}` as never)}
									</StatusTag>
								)
							},
							{
								title: t('releases.themeColumn'),
								render: (_, record) => `${record.theme.themeId} @ ${record.theme.version}`
							},
							{
								title: t('builds.siteSettingsRevision'),
								dataIndex: 'siteSettingsRevision'
							},
							{
								title: t('builds.themeRevision'),
								dataIndex: 'themeSelectionRevision'
							},
							{
								title: t('builds.inputDigests'),
								render: (_, record) =>
									record.inputDigests ? (
										<Typography.Text type="secondary" style={{ fontSize: 12 }}>
											{String(
												(record.inputDigests as Record<string, string>).contentDigest ?? ''
											).slice(7, 19)}
											…
										</Typography.Text>
									) : null
							},
							{
								render: (_, record) => (
									<Space>
										{record.failure ? (
											<Typography.Text type="danger">
												{t('builds.failureStage')}: {record.failure.stage}
											</Typography.Text>
										) : null}
										{record.status === 'succeeded' ? (
											<Button
												size="small"
												loading={opening === record.id}
												disabled={closing === record.id}
												onClick={() => void handlePreview(record.id, record.previewActive)}
											>
												<Trans text={t('builds.preview')} />
											</Button>
										) : null}
										{record.status === 'succeeded' && editable ? (
											<Popconfirm
												title={t('builds.previewCloseConfirm')}
												onConfirm={() => void handleClosePreview(record.id)}
											>
												<Button
													size="small"
													loading={closing === record.id}
													disabled={
														!record.previewActive &&
														opening !== record.id &&
														!previewWindows.current.has(`${siteId}/${record.id}`)
													}
												>
													<Trans text={t('builds.previewClose')} />
												</Button>
											</Popconfirm>
										) : null}
									</Space>
								)
							}
						]}
					/>
				</TablePanel>
			)}
		</div>
	);
}
