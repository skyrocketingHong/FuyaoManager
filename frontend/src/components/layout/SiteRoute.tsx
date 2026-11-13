import type { ReactNode } from 'react';
import { Button, Result } from 'antd';
import { Link } from 'react-router-dom';
import { useCurrentSite } from './SiteBoundary';
import CaddySitePage from '../../pages/sites/CaddySitePage';
import { t } from '../../lib/i18n';
import { allowSitePage } from '../../lib/navigation/workspace';
import { ContentScopeProvider } from '../ContentScopeProvider';
import type { ContentResource } from '../../lib/api/content';

export default function SiteRoute({ page, children }: { page: string; children: ReactNode }) {
	const site = useCurrentSite();
	if (!allowSitePage(site, page)) {
		return (
			<Result
				status="404"
				title={t('caddy.unsupportedPage')}
				extra={
					<Link to={`/sites/${site?.siteId}`}>
						<Button>{t('nav.overview')}</Button>
					</Link>
				}
			/>
		);
	}
	if (page === 'site.caddy') return <CaddySitePage />;
	const resource = page.replace('site.', '') as ContentResource;
	if (site && ['posts', 'albums', 'footprints', 'friends'].includes(resource))
		return (
			<ContentScopeProvider site={site} resource={resource}>
				{children}
			</ContentScopeProvider>
		);
	return children;
}
