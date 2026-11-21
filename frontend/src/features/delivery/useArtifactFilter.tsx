import { PageTabs } from '../../components/PageHeader';
import type { ArtifactState } from '../../lib/api/artifacts';
import { t, type MessageKey } from '../../lib/i18n';
import { queryChoice, usePageQuery } from '../../lib/navigation/query';

export function useArtifactFilter<Row extends { artifactState?: ArtifactState }>(
	rows: readonly Row[],
	label: MessageKey,
	hasOutput: (row: Row) => boolean
) {
	const [params, setParams] = usePageQuery();
	const value = queryChoice(params, 'filter', ['all', 'ready', 'pending'], 'all');
	const visible = rows.filter(
		(row) =>
			value === 'all' ||
			(value === 'pending'
				? row.artifactState === 'deleting' || row.artifactState === 'deleted'
				: (!row.artifactState || row.artifactState === 'available') && hasOutput(row))
	);
	const tabs = (
		<PageTabs
			ariaLabel={t(label)}
			value={value}
			options={[
				{ value: 'all', label: t('artifacts.all') },
				{ value: 'ready', label: t('artifacts.ready') },
				{ value: 'pending', label: t('artifacts.pending') }
			]}
			onChange={(filter) => setParams({ filter: filter === 'all' ? null : filter })}
		/>
	);
	return { visible, tabs };
}
