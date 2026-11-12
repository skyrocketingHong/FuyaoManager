/**
 * PageLoading: the shared waiting indicator in three specs — `page` centers
 * a full page wait, `section` keeps a block-level wait compact, `fill`
 * overlays a host during refreshes that keep existing content visible.
 */
import { Spin } from 'antd';

export default function PageLoading({
	spec = 'page',
	label
}: {
	spec?: 'page' | 'section' | 'fill';
	label?: string;
}) {
	return (
		<div className={`page-loading page-loading--${spec}`} role="status" aria-busy="true">
			<Spin size={spec === 'page' ? 'large' : 'medium'} />
			{label ? <span>{label}</span> : null}
		</div>
	);
}
