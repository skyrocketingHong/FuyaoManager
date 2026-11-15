// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import MarkdownPreview from '../src/features/posts/MarkdownPreview';
import { buildTableOfContents } from '../src/features/posts/markdown';

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

it('generates depth-limited links with matching duplicate slugs and ignores code fences', () => {
	const source = '# 标题\n## Repeat\n### Repeat\n## Repeat\n```md\n# Not a heading\n```';
	expect(buildTableOfContents(source, 2)).toBe(
		'- [标题](#%E6%A0%87%E9%A2%98)\n  - [Repeat](#repeat)\n  - [Repeat](#repeat-2)'
	);
	expect(buildTableOfContents(source, 3)).toContain('    - [Repeat](#repeat-1)');
	expect(buildTableOfContents('## <em>Title</em> ![logo](logo.png)', 3)).toBe('- [Title](#title-)');
});

it('keeps fragment navigation inside the preview and opens ordinary links normally', () => {
	const scroll = vi.fn();
	Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
		configurable: true,
		value: scroll
	});
	render(
		<MarkdownPreview
			source={
				'[Jump](#2020-09-17-%E8%AF%91-windows)\n\n[External](https://example.com)\n\n## 2020-09-17: [译] Windows'
			}
		/>
	);
	const anchor = screen.getByRole('link', { name: 'Jump' });
	expect(anchor.getAttribute('target')).toBeNull();
	fireEvent.click(anchor);
	expect(scroll).toHaveBeenCalledTimes(1);
	expect(document.activeElement).toBe(
		screen.getByRole('heading', { name: '2020-09-17: [译] Windows' })
	);
	expect(screen.getByRole('link', { name: 'External' }).getAttribute('target')).toBe('_blank');
});
