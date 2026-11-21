import { queryChoice, usePageQuery } from '../../lib/navigation/query';
/**
 * SiteConfigPage: the visual site configuration the theme consumes —
 * profile, repository, SEO, services (with the write-only AMap security
 * code), and the social links list. One form state, digest-based saves, 412
 * recovery keeps local edits.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Empty, Input, Space, Tooltip, Typography, message } from 'antd';
import { Info, Plus, X } from 'lucide-react';
import Trans from '../../components/i18n/Trans';
import { PageTabs, usePageHeader } from '../../components/PageHeader';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import PageLoading from '../../components/PageLoading';
import { Panel } from '../../components/Panel';
import {
	getSite,
	getSiteConfig,
	putSiteConfig,
	putSocialLinks,
	type Site,
	type SiteConfig,
	type SocialLink
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';
import { RetryButton } from '../../components/ActionButton';

interface ConfigForm {
	siteUrl: string;
	startDate: string;
	startYear: string;
	profileName: string;
	birthDate: string;
	avatarUrl: string;
	roleZh: string;
	roleEn: string;
	quoteZh: string;
	quoteEn: string;
	repoName: string;
	repoUrl: string;
	repoOwner: string;
	seoAuthor: string;
	seoDescription: string;
	seoKeywords: string;
	seoTwitterId: string;
	wallpaperUrl: string;
	wallpaperApiUrl: string;
	wallpaperMetadataApiUrl: string;
	githubProxyUrl: string;
	analyticsProxyUrl: string;
	codingActivityProxyUrl: string;
	codingLanguagesProxyUrl: string;
	amapBrowserKey: string;
	amapServiceHost: string;
	amapSecurityCode: string;
}

export default function SiteConfigPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [config, setConfig] = useState<SiteConfig | null>(null);
	const [links, setLinks] = useState<SocialLink[]>([]);
	const [linksDigest, setLinksDigest] = useState('');
	const [digest, setDigest] = useState('');
	const [securitySet, setSecuritySet] = useState(false);
	const [effectiveOrigin, setEffectiveOrigin] = useState('');
	const [form, setForm] = useState<ConfigForm | null>(null);
	const [securityInput, setSecurityInput] = useState('');
	const [dirty, setDirty] = useState(false);
	const [linksDirty, setLinksDirty] = useState(false);
	const [saving, setSaving] = useState(false);
	const [conflict, setConflict] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [validationError, setValidationError] = useState<MessageKey | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	const epoch = useRef(0);

	const toForm = (cfg: SiteConfig): ConfigForm => ({
		siteUrl: cfg.site?.url ?? '',
		startDate: cfg.site?.startDate ?? '',
		startYear: cfg.site?.startYear === undefined ? '' : String(cfg.site.startYear),
		profileName: cfg.profile?.name ?? '',
		birthDate: cfg.profile?.birthDate ?? '',
		avatarUrl: cfg.profile?.avatarUrl ?? '',
		roleZh: cfg.profile?.roles?.['zh-CN'] ?? '',
		roleEn: cfg.profile?.roles?.['en-US'] ?? '',
		quoteZh: cfg.profile?.quotes?.['zh-CN'] ?? '',
		quoteEn: cfg.profile?.quotes?.['en-US'] ?? '',
		repoName: cfg.repository?.name ?? '',
		repoUrl: cfg.repository?.url ?? '',
		repoOwner: cfg.repository?.owner ?? '',
		seoAuthor: cfg.seo?.author ?? '',
		seoDescription: cfg.seo?.description ?? '',
		seoKeywords: (cfg.seo?.keywords ?? []).join(', '),
		seoTwitterId: cfg.seo?.twitterId ?? '',
		wallpaperUrl: cfg.services?.wallpaper?.defaultUrl ?? '',
		wallpaperApiUrl: cfg.services?.wallpaper?.apiUrl ?? '',
		wallpaperMetadataApiUrl: cfg.services?.wallpaper?.metadataApiUrl ?? '',
		githubProxyUrl: cfg.services?.githubProxyUrl ?? '',
		analyticsProxyUrl: cfg.services?.analyticsProxyUrl ?? '',
		codingActivityProxyUrl: cfg.services?.codingActivityProxyUrl ?? '',
		codingLanguagesProxyUrl: cfg.services?.codingLanguagesProxyUrl ?? '',
		amapBrowserKey: cfg.services?.amap?.browserKey ?? '',
		amapServiceHost: cfg.services?.amap?.serviceHost ?? '',
		amapSecurityCode: ''
	});

	const load = useCallback(async () => {
		if (!siteId) return;
		const my = ++epoch.current;
		try {
			const [cfgEnv, linkEnv] = await Promise.all([
				getSiteConfig(siteId),
				(async () => (await import('../../lib/api/management')).getSocialLinks(siteId))()
			]);
			if (my !== epoch.current) return;
			setConfig(cfgEnv.config);
			setDigest(cfgEnv.digest);
			setSecuritySet(cfgEnv.amapSecurityCodeSet);
			setEffectiveOrigin(cfgEnv.effectiveOrigin);
			setForm(toForm(cfgEnv.config));
			setLinks(linkEnv.links);
			setLinksDigest(linkEnv.digest);
			setDirty(false);
			setLinksDirty(false);
			setConflict(false);
			setSecurityInput('');
			setErrorKey(null);
			setValidationError(null);
		} catch (err) {
			if (my !== epoch.current) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key ?? 'error.generic');
		}
	}, [siteId, navigate]);

	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setConfig(null);
		setForm(null);
		setDirty(false);
		setLinksDirty(false);
		setSecurityInput('');
		setErrorKey(null);
		setSaving(false);
		let cancelled = false;
		getSite(siteId)
			.then((s) => {
				if (!cancelled) setSite(s);
			})
			.catch(() => {});
		void load();
		return () => {
			epoch.current++;
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [siteId]);

	function patch(delta: Partial<ConfigForm>) {
		setDirty(true);
		setValidationError(null);
		setForm((prev) => (prev ? { ...prev, ...delta } : prev));
	}

	function field(label: string, key: keyof ConfigForm, placeholder = '') {
		return (
			<label className="field">
				<span className="field-label">{label}</span>
				<Input
					size="small"
					value={form ? form[key] : ''}
					placeholder={placeholder}
					disabled={!site || site.role === 'viewer' || saving}
					onChange={(e) => patch({ [key]: e.target.value } as Partial<ConfigForm>)}
				/>
			</label>
		);
	}

	async function handleSave() {
		if (!siteId || !form || !config || saving) return;
		for (const [value, previous] of [
			[form.startDate, config.site.startDate ?? ''],
			[form.birthDate, config.profile.birthDate ?? '']
		]) {
			if (
				value !== previous &&
				value &&
				(!/^\d{4}-\d{2}-\d{2}$/.test(value) ||
					!Number(value.slice(0, 4)) ||
					Number.isNaN(Date.parse(`${value}T00:00:00Z`)) ||
					new Date(`${value}T00:00:00Z`).toISOString().slice(0, 10) !== value)
			) {
				setValidationError('config.invalidDate');
				return;
			}
		}
		if (
			form.startYear !== String(config.site.startYear ?? '') &&
			form.startYear &&
			!/^[1-9]\d{0,3}$/.test(form.startYear)
		) {
			setValidationError('config.invalidYear');
			return;
		}
		try {
			const url = new URL(form.siteUrl);
			if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) throw new Error();
		} catch {
			setValidationError('config.invalidUrl');
			return;
		}
		setValidationError(null);
		const requestEpoch = epoch.current;
		setSaving(true);
		try {
			const next: SiteConfig = {
				...config,
				site: {
					...config.site,
					url: form.siteUrl,
					startDate: form.startDate,
					startYear: form.startYear === '' ? undefined : Number(form.startYear)
				},
				profile: {
					...config.profile,
					name: form.profileName,
					birthDate: form.birthDate,
					avatarUrl: form.avatarUrl,
					roles: { ...config.profile.roles, 'zh-CN': form.roleZh, 'en-US': form.roleEn },
					quotes: { ...config.profile.quotes, 'zh-CN': form.quoteZh, 'en-US': form.quoteEn }
				},
				repository: {
					...config.repository,
					name: form.repoName,
					url: form.repoUrl,
					owner: form.repoOwner
				},
				seo: {
					...config.seo,
					twitterId: form.seoTwitterId,
					author: form.seoAuthor,
					description: form.seoDescription,
					keywords: form.seoKeywords
						.split(',')
						.map((s) => s.trim())
						.filter(Boolean)
				},
				services: {
					...config.services,
					wallpaper: {
						...config.services?.wallpaper,
						defaultUrl: form.wallpaperUrl,
						apiUrl: form.wallpaperApiUrl,
						metadataApiUrl: form.wallpaperMetadataApiUrl
					},
					githubProxyUrl: form.githubProxyUrl,
					analyticsProxyUrl: form.analyticsProxyUrl,
					codingActivityProxyUrl: form.codingActivityProxyUrl,
					codingLanguagesProxyUrl: form.codingLanguagesProxyUrl,
					amap: {
						...config.services?.amap,
						browserKey: form.amapBrowserKey,
						serviceHost: form.amapServiceHost
					}
				}
			};
			// The security code is write-only: send it only when the user
			// typed something; an explicit empty field keeps the stored value.
			const securityCode = securityInput !== '' ? securityInput : undefined;
			const saved = await putSiteConfig(siteId, next, digest, securityCode);
			if (requestEpoch !== epoch.current) return;
			setDigest(saved.digest);
			setConfig(saved.config);
			setForm(toForm(saved.config));
			setSecuritySet(saved.amapSecurityCodeSet ?? false);
			setSecurityInput('');
			setDirty(false);
			setConflict(false);
			void messageApi.success(t('config.saved'));
		} catch (err) {
			if (requestEpoch !== epoch.current) return;
			if (apiErrorKey(err) === 'error.conflict') {
				setConflict(true);
			} else {
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
			}
		} finally {
			if (requestEpoch === epoch.current) setSaving(false);
		}
	}

	async function handleSaveLinks() {
		if (!siteId || saving) return;
		const requestEpoch = epoch.current;
		setSaving(true);
		try {
			const saved = await putSocialLinks(siteId, links, linksDigest);
			if (requestEpoch !== epoch.current) return;
			setLinksDigest(saved.digest);
			setLinks(saved.links);
			setLinksDirty(false);
			void messageApi.success(t('config.saved'));
		} catch (err) {
			if (requestEpoch !== epoch.current) return;
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			if (requestEpoch === epoch.current) setSaving(false);
		}
	}

	const [params, setParams] = usePageQuery();
	const tab = queryChoice(params, 'tab', ['general', 'services', 'links'], 'general');
	const tabs = (
		<PageTabs
			value={tab}
			ariaLabel={t('nav.config')}
			disabled={saving}
			options={[
				{ value: 'general', label: t('config.generalTab') },
				{ value: 'services', label: t('config.servicesTab') },
				{ value: 'links', label: t('config.linksTab') }
			]}
			onChange={(value) => setParams({ tab: value === 'general' ? null : value })}
		/>
	);
	const editable = site !== null && (site.role === 'owner' || site.role === 'editor');
	useUnsavedGuard(
		editable && (dirty || linksDirty || securityInput !== '' || saving),
		(current, next) => !saving && current.pathname === next.pathname
	);

	const headerActions = (
		<Space size={8}>
			{conflict ? (
				<Button disabled={saving} onClick={() => void load()}>
					{t('posts.conflictAction')}
				</Button>
			) : null}
			<Button
				type="primary"
				loading={saving}
				disabled={!editable || (tab === 'links' ? !linksDirty : !dirty && securityInput === '')}
				onClick={() => void (tab === 'links' ? handleSaveLinks() : handleSave())}
			>
				<Trans text={t('common.save')} />
			</Button>
		</Space>
	);
	const headerActive = usePageHeader({ content: tabs, actions: headerActions }, form !== null);

	if (form === null) {
		return (
			<div className="page-stack">
				{contextHolder}
				{errorKey ? (
					<Alert
						type="error"
						showIcon
						title={t(errorKey)}
						action={<RetryButton onClick={() => void load()} />}
					/>
				) : (
					<PageLoading spec="page" label={t('common.loading')} />
				)}
			</div>
		);
	}

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div className="table-panel-header">
					{tabs}
					{headerActions}
				</div>
			) : null}
			{conflict ? (
				<Alert
					type="warning"
					showIcon
					title={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{validationError && <Alert type="error" showIcon title={t(validationError)} />}
			{tab === 'general' && (
				<div className="workspace-grid">
					<Panel title={t('config.originSection')}>
						<div className="form-grid">
							{field(t('config.siteUrl'), 'siteUrl', 'https://…')}
							{field(t('config.startDate'), 'startDate', 'YYYY-MM-DD')}
							{field(t('config.startYear'), 'startYear', '2026')}
						</div>
						{effectiveOrigin ? (
							<>
								<Space>
									<span className="version-text">{effectiveOrigin}</span>
									<Tooltip title={t('config.originHint')}>
										<Button
											type="text"
											size="small"
											icon={<Info size={14} />}
											aria-label={t('config.originSection')}
										/>
									</Tooltip>
								</Space>
							</>
						) : (
							<Typography.Text type="secondary">{t('config.originPending')}</Typography.Text>
						)}
					</Panel>
					<Panel className="workspace-half" title={t('config.profileSection')}>
						<div className="form-grid">
							{field(t('config.profileName'), 'profileName')}
							{field(t('config.avatar'), 'avatarUrl', 'https://…')}
							{field(t('config.birthDate'), 'birthDate', '2000-01-01')}
						</div>
						<div className="form-grid">
							{field(`${t('config.role')} (zh)`, 'roleZh')}
							{field(`${t('config.role')} (en)`, 'roleEn')}
						</div>
						<div className="form-grid">
							{field(`${t('config.quote')} (zh)`, 'quoteZh')}
							{field(`${t('config.quote')} (en)`, 'quoteEn')}
						</div>
					</Panel>
					<Panel className="workspace-half" title={t('config.seoSection')}>
						<div className="form-grid">
							{field(t('config.seoAuthor'), 'seoAuthor')}
							{field(t('config.repoName'), 'repoName')}
							{field(t('config.repoOwner'), 'repoOwner')}
						</div>
						{field(t('config.repoUrl'), 'repoUrl', 'https://github.com/…')}
						{field(t('config.seoDescription'), 'seoDescription')}
						{field(t('config.seoKeywords'), 'seoKeywords', 'Svelte, Go')}
						{field(t('config.twitterId'), 'seoTwitterId')}
					</Panel>
				</div>
			)}
			{tab === 'services' && (
				<Panel title={t('config.servicesSection')}>
					{field(t('config.wallpaper'), 'wallpaperUrl', 'https://…')}
					<div className="form-grid">
						{field(t('config.wallpaperApiUrl'), 'wallpaperApiUrl')}
						{field(t('config.wallpaperMetadataApiUrl'), 'wallpaperMetadataApiUrl')}
					</div>
					<div className="form-grid">
						{field(t('config.githubProxyUrl'), 'githubProxyUrl')}
						{field(t('config.analyticsProxyUrl'), 'analyticsProxyUrl')}
						{field(t('config.codingActivityProxyUrl'), 'codingActivityProxyUrl')}
						{field(t('config.codingLanguagesProxyUrl'), 'codingLanguagesProxyUrl')}
					</div>
					<div className="form-grid">
						{field(t('config.amapKey'), 'amapBrowserKey')}
						{field(t('config.amapServiceHost'), 'amapServiceHost', 'https://…/_AMapService')}
						<label className="field">
							<span className="field-label">
								AMap {t('config.securityCode')} {securitySet ? `(${t('config.securitySet')})` : ''}
							</span>
							<Input.Password
								size="small"
								value={securityInput}
								disabled={!editable || saving}
								placeholder={t('config.securityPlaceholder')}
								onChange={(e) => setSecurityInput(e.target.value)}
							/>
						</label>
					</div>
				</Panel>
			)}
			{tab === 'links' && (
				<Panel
					title={t('config.socialSection')}
					tags={
						editable ? (
							<Space>
								<Button
									size="small"
									icon={<Plus size={15} />}
									disabled={saving}
									onClick={() => {
										setLinks([...links, { name: '', href: '' }]);
										setLinksDirty(true);
									}}
								>
									{t('config.addLink')}
								</Button>
							</Space>
						) : null
					}
				>
					{links.length === 0 ? (
						<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('config.noLinks')} />
					) : null}
					{links.map((link, index) => (
						<div key={index} className="social-link-row">
							<Input
								size="small"
								aria-label={`${t('friends.fieldName')} ${index + 1}`}
								placeholder={t('friends.fieldName')}
								value={link.name}
								disabled={!editable || saving}
								onChange={(e) => {
									const next = links.map((l, i) =>
										i === index ? { ...l, name: e.target.value } : l
									);
									setLinks(next);
									setLinksDirty(true);
								}}
							/>
							<Input
								size="small"
								aria-label={`${t('config.linkUrl')} ${index + 1}`}
								placeholder="https://… / mailto:…"
								value={link.href}
								disabled={!editable || saving}
								onChange={(e) => {
									const next = links.map((l, i) =>
										i === index ? { ...l, href: e.target.value } : l
									);
									setLinks(next);
									setLinksDirty(true);
								}}
							/>
							<Input
								size="small"
								aria-label={`${t('config.linkIcon')} ${index + 1}`}
								placeholder="icon"
								value={link.icon ?? ''}
								disabled={!editable || saving}
								onChange={(e) => {
									const next = links.map((l, i) =>
										i === index ? { ...l, icon: e.target.value } : l
									);
									setLinks(next);
									setLinksDirty(true);
								}}
							/>
							{editable ? (
								<Button
									size="small"
									type="text"
									danger
									disabled={saving}
									aria-label={`${t('common.delete')} ${link.name || index + 1}`}
									icon={<X size={15} />}
									onClick={() => {
										setLinks(links.filter((_, i) => i !== index));
										setLinksDirty(true);
									}}
								/>
							) : null}
						</div>
					))}
				</Panel>
			)}
		</div>
	);
}
