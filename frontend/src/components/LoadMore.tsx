import { useEffect, type RefObject } from 'react';
import { Button, type TableRef } from 'antd';
import { t } from '../lib/i18n';

export type LoadMoreControls = {
	active: boolean;
	loading: boolean;
	onLoad: () => void;
};

type LoadMoreProps = LoadMoreControls & {
	tableRef: RefObject<TableRef>;
	rows: readonly object[] | undefined;
};

export default function LoadMore({ active, loading, onLoad, tableRef, rows }: LoadMoreProps) {
	useEffect(() => {
		if (!active || loading || !rows?.length || typeof IntersectionObserver === 'undefined') return;
		const lastRow = tableRef.current?.nativeElement.querySelector('tbody > tr[data-row-key]:last-of-type');
		if (!lastRow) return;
		let observing = true;
		const observer = new IntersectionObserver((entries) => {
			if (!observing || !entries.some((entry) => entry.isIntersecting)) return;
			stop();
			onLoad();
		}, { rootMargin: '200px 0px' });
		function stop() {
			if (!observing) return;
			observing = false;
			observer.disconnect();
		}
		observer.observe(lastRow);
		return stop;
	}, [active, loading, onLoad, tableRef, rows]);
	if (!active) return null;
	return (
		<div className="table-load-more">
			<Button loading={loading} onClick={onLoad}>{t('common.loadMore')}</Button>
		</div>
	);
}
