import type { ContentCounts } from '../lib/api/content';
import { t, type MessageKey } from '../lib/i18n';

const counters: (keyof ContentCounts)[] = ['posts', 'albums', 'photos', 'places', 'friends'];
export default function ContentStatistics({ counts }: { counts: ContentCounts }) {
	return (
		<dl className="fact-grid">
			{counters.map((key) => (
				<div key={key}>
					<dt>{t(`content.count.${key}` as MessageKey)}</dt>
					<dd>{counts[key]}</dd>
				</div>
			))}
		</dl>
	);
}
