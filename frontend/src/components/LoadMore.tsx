import { useEffect } from 'react';
import { Button } from 'antd';
import { t } from '../lib/i18n';

export default function LoadMore({ active, loading, onLoad, table, rowCount }: { active: boolean; loading: boolean; onLoad: () => void; table: string; rowCount: number }) {
	useEffect(() => {
		if (!active || loading || typeof IntersectionObserver === 'undefined') return;
		const lastRow = document.querySelector(`${table} tbody tr[data-row-key]:last-of-type`);
		if (!lastRow) return;
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((entry) => entry.isIntersecting)) onLoad();
		}, { rootMargin: '200px 0px' });
		observer.observe(lastRow);
		return () => observer.disconnect();
	}, [active, loading, onLoad, table, rowCount]);
	if (!active) return null;
	return <div className="posts-load-more"><Button loading={loading} onClick={onLoad}>{t('posts.loadMore')}</Button></div>;
}
