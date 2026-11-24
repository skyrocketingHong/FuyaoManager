/**
 * PageLoading: the shared waiting indicator in three specs — `page` centers
 * a full page wait, `section` keeps a block-level wait compact, `fill`
 * overlays a host during refreshes that keep existing content visible.
 * Every spec announces a status and carries a text label by default
 * (robot-platform's LoadingState); the spinner itself stays hidden from
 * assistive technology so the label is the single announcement.
 */
import { Spin } from 'antd';
import { t } from '../lib/i18n';

export default function PageLoading({
	spec = 'page',
	label
}: {
	spec?: 'page' | 'section' | 'fill';
	label?: string;
}) {
	return (
		<div
			className={`page-loading page-loading--${spec}`}
			role="status"
			aria-live="polite"
			aria-busy="true"
		>
			<Spin size={spec === 'page' ? 'large' : 'medium'} aria-hidden="true" />
			<span className="page-loading-label">{label ?? t('common.loading')}</span>
		</div>
	);
}
