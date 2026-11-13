import type { ReactNode } from 'react';
import { Button, Result } from 'antd';
import { Link } from 'react-router-dom';
import { useCurrentSite } from './SiteBoundary';
import CaddySitePage from '../../pages/sites/CaddySitePage';
import { t } from '../../lib/i18n';

export default function SiteRoute({ page, children }: { page: string; children: ReactNode }) {
	const site = useCurrentSite();
	const kind = site?.kind ?? 'theme';
	if (kind !== 'theme') {
		if (['site.overview', 'site.caddy', 'site.settings'].includes(page)) return <CaddySitePage />;
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
	if (page === 'site.caddy') return <Result status="404" title={t('caddy.unsupportedPage')} />;
	return children;
}
