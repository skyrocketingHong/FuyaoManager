/**
 * PageBoundary: an error boundary around one route's page slot. A render
 * failure inside a page is reported within the workspace while the shell —
 * sidebar, top bar and session — stays mounted, so the operator can retry
 * the page or leave through the navigation. Without it the router's
 * errorElement replaces the whole console for a single page's fault.
 *
 * The boundary clears its failed state whenever the location changes, so the
 * route slot can be reused for the next page.
 */
import {
	Component,
	useSyncExternalStore,
	type ErrorInfo,
	type ReactNode
} from 'react';
import { Link, useLocation } from 'react-router-dom';
import { Button, Result, Space } from 'antd';
import { i18nSnapshot, subscribeI18n, t } from '../lib/i18n';

function PageError({ onRetry }: { onRetry: () => void }) {
	// Re-render on locale switches so the message follows the interface.
	useSyncExternalStore(subscribeI18n, i18nSnapshot);
	return (
		<section className="panel" role="alert">
			<div className="panel-body">
				<Result
					status="error"
					title={t('app.pageError')}
					extra={
						<Space wrap>
							<Button type="primary" onClick={onRetry}>
								{t('common.retry')}
							</Button>
							<Link to="/sites">
								<Button>{t('nav.platformHome')}</Button>
							</Link>
						</Space>
					}
				/>
			</div>
		</section>
	);
}

interface BoundaryProps {
	resetKey: string;
	children: ReactNode;
}

class Boundary extends Component<BoundaryProps, { failed: boolean }> {
	state = { failed: false };

	static getDerivedStateFromError(): { failed: boolean } {
		return { failed: true };
	}

	componentDidCatch(error: Error, info: ErrorInfo) {
		console.error('Page render failed', error, info.componentStack);
	}

	componentDidUpdate(previous: BoundaryProps) {
		if (this.state.failed && previous.resetKey !== this.props.resetKey)
			this.setState({ failed: false });
	}

	render() {
		if (!this.state.failed) return this.props.children;
		return <PageError onRetry={() => this.setState({ failed: false })} />;
	}
}

export default function PageBoundary({ children }: { children: ReactNode }) {
	const location = useLocation();
	return (
		<Boundary resetKey={location.key}>
			{children}
		</Boundary>
	);
}
