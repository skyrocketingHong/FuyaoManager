import type { ReactNode } from 'react';
import { Button, Result } from 'antd';
import { Link } from 'react-router-dom';
import { useCurrentSite } from './SiteBoundary';
import { t } from '../../lib/i18n';
import { allowSitePage } from '../../lib/navigation/workspace';
import { ContentScopeProvider } from '../ContentScopeProvider';
import type { ContentResource } from '../../lib/api/content';

/** Site-scope gate for one page: pages the site cannot host stop here with a
 * link back to its overview. Content pages additionally receive the site's
 * content scope; every other page (the Caddy page among them) renders as is,
 * so this route stays independent of the page implementations. */
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
	const resource = page.replace('site.', '') as ContentResource;
	if (site && ['posts', 'albums', 'footprints', 'friends'].includes(resource))
		return (
			<ContentScopeProvider site={site} resource={resource} fill>
				{children}
			</ContentScopeProvider>
		);
	return children;
}
