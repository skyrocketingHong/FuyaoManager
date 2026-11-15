/**
 * PageHeader tests: page-published top-bar content follows the provider
 * scope, disappears on unmount, and a scope change (route/site/account
 * switch) invalidates stale records even when their cleanup runs late.
 */
// @vitest-environment jsdom
import { describe, expect, it, beforeAll, afterEach } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { ConfigProvider } from 'antd';
import {
	PageHeaderProvider,
	usePageHeader,
	usePageHeaderContent
} from '../src/components/PageHeader';

beforeAll(() => {
	if (!window.matchMedia) {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			value: (query: string) => ({
				matches: false,
				media: query,
				onchange: null,
				addListener: () => undefined,
				removeListener: () => undefined,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				dispatchEvent: () => false
			})
		});
	}
});

afterEach(cleanup);

function Slot() {
	const header = usePageHeaderContent();
	return (
		<div>
			<span data-testid="leading">{header?.leading}</span>
			<span data-testid="content">{header?.content}</span>
			<span data-testid="actions">{header?.actions}</span>
		</div>
	);
}

function Publisher({ tag, enabled = true }: { tag: string; enabled?: boolean }) {
	usePageHeader(
		{
			leading: <button>back-{tag}</button>,
			content: <span>content-{tag}</span>,
			actions: <button>action-{tag}</button>
		},
		enabled
	);
	return null;
}

function Harness({ scope, pages }: { scope: string; pages: string[] }) {
	return (
		<ConfigProvider>
			<PageHeaderProvider scope={scope}>
				<Slot />
				{pages.map((page) => (
					<Publisher key={page} tag={page} />
				))}
			</PageHeaderProvider>
		</ConfigProvider>
	);
}

describe('PageHeader slots', () => {
	it('renders published content and actions into the top bar', () => {
		render(<Harness scope="u1:/sites" pages={['a']} />);
		expect(screen.getByText('content-a')).toBeDefined();
		expect(screen.getByText('back-a')).toBeDefined();
		expect(screen.getByText('action-a')).toBeDefined();
	});

	it('removes the entry when the publishing page unmounts', async () => {
		function Switcher() {
			const [page, setPage] = useState('a');
			return (
				<>
					<button onClick={() => setPage('b')}>switch</button>
					<Harness scope="u1:/sites" pages={[page]} />
				</>
			);
		}
		render(<Switcher />);
		expect(screen.getByText('content-a')).toBeDefined();
		fireEvent.click(screen.getByText('switch'));
		await waitFor(() => {
			expect(screen.queryByText('content-a')).toBeNull();
		});
		expect(screen.getByText('content-b')).toBeDefined();
	});

	it('drops stale records published under an older scope', () => {
		const { rerender } = render(<Harness scope="u1:/sites/a" pages={['a']} />);
		expect(screen.getByText('content-a')).toBeDefined();
		// Simulates a fast site switch: the provider scope changed before the
		// old page's cleanup ran — the stale record must not render.
		rerender(<Harness scope="u1:/sites/b" pages={[]} />);
		expect(screen.queryByText('content-a')).toBeNull();
	});
});
