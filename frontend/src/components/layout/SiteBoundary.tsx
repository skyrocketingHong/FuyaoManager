/**
 * SiteBoundary: the site-context gate for every `/sites/:siteId/*` page. The
 * URL siteId is the authoritative site identity — the boundary loads exactly
 * that site and renders distinct states for loading, not-found/no-permission
 * (the API answers 404 for both), and transient load failure (with retry).
 * It never bounces back to the list and never substitutes another site.
 */
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Button, Result, Spin } from 'antd';
import { ApiError } from '../../lib/api/client';
import { getSite, type Site } from '../../lib/api/management';
import { t } from '../../lib/i18n';
import { RetryButton } from '../ActionButton';

const SiteContext = createContext<Site | null>(null);

/** The loaded site of the current `/sites/:siteId/*` scope (null outside). */
export function useCurrentSite(): Site | null {
	return useContext(SiteContext);
}

type Phase =
	| { kind: 'loading' }
	| { kind: 'ready'; site: Site }
	| { kind: 'error'; notFound: boolean };

export default function SiteBoundary({ children }: { children: ReactNode }) {
	const { siteId } = useParams<{ siteId: string }>();
	const [phase, setPhase] = useState<Phase>({ kind: 'loading' });
	const [epoch, setEpoch] = useState(0);

	useEffect(() => {
		if (!siteId) return;
		let cancelled = false;
		setPhase({ kind: 'loading' });
		getSite(siteId)
			.then((site) => {
				if (!cancelled) setPhase({ kind: 'ready', site });
			})
			.catch((err) => {
				if (cancelled) return;
				const notFound = err instanceof ApiError && err.status === 404;
				setPhase({ kind: 'error', notFound });
			});
		return () => {
			cancelled = true;
		};
	}, [siteId, epoch]);

	if (phase.kind === 'loading') {
		return (
			<div className="boundary-loading" role="status">
				<Spin size="large" />
			</div>
		);
	}

	if (phase.kind === 'error') {
		return (
			<Result
				status={phase.notFound ? '404' : 'warning'}
				title={t(phase.notFound ? 'site.boundary.notFoundTitle' : 'site.boundary.loadTitle')}
				subTitle={t(phase.notFound ? 'site.boundary.notFoundBody' : 'site.boundary.loadBody')}
				extra={
					<>
						{!phase.notFound ? (
							<RetryButton type="primary" onClick={() => setEpoch((n) => n + 1)} />
						) : null}
						<Link to="/sites">
							<Button>{t('site.boundary.backToSites')}</Button>
						</Link>
					</>
				}
			/>
		);
	}

	return <SiteContext.Provider value={phase.site}>{children}</SiteContext.Provider>;
}
