import { createContext, useCallback, useContext, type ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Alert, Button } from 'antd';
import { useAdminQuery } from '../lib/admin/requests';
import {
	getContentSources,
	getPlatformContent,
	platformContent,
	type ContentResource,
	type ContentTarget,
	type PlatformContentMeta
} from '../lib/api/content';
import type { Site } from '../lib/api/management';
import { t } from '../lib/i18n';
import PageLoading from './PageLoading';

interface ContentScope {
	target: ContentTarget | undefined;
	key: string;
	canEdit?: boolean;
	amap?: PlatformContentMeta['amap'];
}
const Context = createContext<ContentScope | null>(null);

export function ContentScopeProvider({
	site,
	resource,
	fill = false,
	children
}: {
	site?: Site;
	resource: ContentResource;
	/** Opt-in workspace fill: the wrapper stack joins the height chain so
	 * scrolling surfaces inside it can span the full workspace. Content
	 * resources without a stretching surface keep the natural layout. */
	fill?: boolean;
	children: ReactNode;
}) {
	const load = useCallback(
		async (signal: AbortSignal) => {
			if (!site) return { metadata: await getPlatformContent(signal), shared: false };
			const { data } = await getContentSources(site.siteId, signal);
			const shared = data.sources[resource] === 'platform';
			const metadata =
				shared && resource === 'footprints' ? await getPlatformContent(signal) : undefined;
			return { shared, metadata };
		},
		[site?.siteId, resource]
	);
	const query = useAdminQuery(load);
	const shared = query.data?.shared === true;
	const target = site?.siteId ?? platformContent;
	const canEdit =
		!!query.data &&
		!query.error &&
		(site ? site.role !== 'viewer' && !shared : query.data.metadata?.canEdit === true);
	return (
		<Context.Provider
			value={{ target, key: site?.siteId ?? 'platform', canEdit, amap: query.data?.metadata?.amap }}
		>
			<div className={fill ? 'page-stack page-stack--fill' : 'page-stack'}>
				{query.error && (
					<Alert
						type="error"
						showIcon
						title={t(query.error)}
						action={<Button onClick={query.reload}>{t('common.retry')}</Button>}
					/>
				)}
				{shared && (
					<Alert
						type="info"
						showIcon
						title={t('content.shared')}
						action={<Link to={`/content/${resource}`}>{t('content.openPlatform')}</Link>}
					/>
				)}
				{!query.data && query.loading && <PageLoading />}
				{query.data && children}
			</div>
		</Context.Provider>
	);
}

export function useContentScope(): ContentScope {
	const context = useContext(Context);
	const { siteId } = useParams<{ siteId: string }>();
	return context ?? { target: siteId, key: siteId ?? 'unresolved' };
}
