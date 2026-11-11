import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Alert, Card, Col, Empty, Row, Spin, Tag } from 'antd';
import { listSites, type Site } from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import { useSyncExternalStore } from 'react';

export default function SitesPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [sites, setSites] = useState<Site[] | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	useEffect(() => {
		if (session.status === 'anonymous') {
			navigate('/login');
			return;
		}
		if (session.status !== 'authenticated') return;
		let cancelled = false;
		listSites()
			.then((list) => {
				if (!cancelled) setSites(list);
			})
			.catch((err) => {
				if (cancelled) return;
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') {
					navigate('/login');
					return;
				}
				setErrorKey(key);
			});
		return () => {
			cancelled = true;
		};
	}, [session.status, navigate]);

	if (sites === null && errorKey === null) {
		return (
			<p role="status">
				<Spin /> {t('common.loading')}
			</p>
		);
	}
	if (errorKey !== null) {
		return <Alert type="error" showIcon message={t(errorKey ?? 'error.generic')} />;
	}
	if (sites !== null && sites.length === 0) {
		return (
			<Card>
				<Empty description={t('sites.empty')} />
			</Card>
		);
	}
	return (
		<>
			<h1>{t('sites.title')}</h1>
			<Row gutter={[16, 16]}>
				{sites?.map((site) => (
					<Col key={site.siteId} xs={24} md={12}>
						<Card
							title={site.name}
							hoverable
							extra={<Tag>{t(`role.${site.role}` as never)}</Tag>}
							onClick={() => navigate(`/sites/${site.siteId}`)}
						>
							<dl>
								<dt>{t('site.slug')}</dt>
								<dd>{site.slug}</dd>
								<dt>{t('site.locale')}</dt>
								<dd>{site.defaultLocale}</dd>
							</dl>
							<Link to={`/sites/${site.siteId}`}>{t('sites.open')}</Link>
						</Card>
					</Col>
				))}
			</Row>
		</>
	);
}
