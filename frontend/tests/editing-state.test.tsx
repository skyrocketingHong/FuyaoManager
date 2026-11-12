// @vitest-environment jsdom
/**
 * Editing-state regressions (R5): mounted React coverage for the acceptance
 * scenarios — unsaved edits made while a friends save is pending stay dirty
 * and savable; a 412 sync on the posts editor refreshes the baseline and
 * ETag without overwriting locally-touched fields; a viewer cannot write;
 * late list responses from another site never land in the current page.
 */
import React from 'react';
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { render, screen, fireEvent, waitFor, cleanup, act } from '@testing-library/react';
import FriendsPage from '../src/pages/sites/FriendsPage';
import PostsPage from '../src/pages/sites/PostsPage';
import * as api from '../src/lib/api/management';
import type { Site } from '../src/lib/api/management';
import { initLocale, t } from '../src/lib/i18n';
import { ApiError } from '../src/lib/api/client';

vi.mock('../src/lib/api/management', async (importOriginal) => ({
	...(await importOriginal<object>()),
	getSite: vi.fn(),
	getFriends: vi.fn(),
	putFriends: vi.fn(),
	listPosts: vi.fn(),
	getPost: vi.fn(),
	putPost: vi.fn()
}));

const site: Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Blog',
	description: '',
	defaultLocale: 'en-US' as 'en-US' | 'zh-CN',
	visibility: 'private',
	provisionState: 'ready',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01',
	updatedAt: '2026-01-01'
};

const post = {
	path: 'tech/hello.md',
	category: 'tech',
	slug: 'hello',
	title: 'Original title',
	date: '2026-11-12T10:00:00.000Z',
	draft: true,
	categories: [],
	tags: [],
	body: 'original',
	digest: 'sha256:' + 'a'.repeat(64)
};

const compact = (s: string): string => s.replace(/\s/g, '');
function button(text: string): HTMLButtonElement {
	const b = Array.from(document.querySelectorAll('button')).find(
		(b) => compact(b.textContent ?? '') === compact(text)
	);
	if (!b) throw new Error('button missing: ' + text);
	return b as HTMLButtonElement;
}

beforeAll(() =>
	Object.defineProperty(window, 'matchMedia', {
		value: (query: string) => ({
			matches: false,
			media: query,
			onchange: null,
			addListener() {},
			removeListener() {},
			addEventListener() {},
			removeEventListener() {},
			dispatchEvent() {
				return false;
			}
		})
	})
);
beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(api.getSite).mockResolvedValue(site);
	initLocale();
});
afterEach(cleanup);

function mount(element: React.ReactElement, section: string, siteId = site.siteId) {
	render(
		<MemoryRouter initialEntries={[`/sites/${siteId}/${section}`]}>
			<Routes>
				<Route path={'/sites/:siteId/' + section} element={element} />
			</Routes>
		</MemoryRouter>
	);
}

describe('editing state (R5)', () => {
	it('retains unsaved state for edits made while a friends save is pending', async () => {
		let resolveSave: (x: unknown) => void = () => {};
		const original = { name: 'Original friend', url: 'https://friend.test' };
		vi.mocked(api.getFriends).mockResolvedValue({
			friends: { online: [original], offline: [] },
			digest: 'd1'
		});
		vi.mocked(api.putFriends).mockImplementation(
			() =>
				new Promise((resolve) => {
					resolveSave = resolve as (x: unknown) => void;
				})
		);
		mount(<FriendsPage />, 'friends');
		const input = await screen.findByDisplayValue('Original friend');
		await waitFor(() => expect(input.hasAttribute('disabled')).toBe(false));

		fireEvent.change(input, { target: { value: 'Submitted A' } });
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(api.putFriends).toHaveBeenCalledTimes(1));

		// The user keeps editing while the request is in flight.
		fireEvent.change(input, { target: { value: 'New unsaved B' } });
		await act(async () => {
			resolveSave({
				friends: { online: [{ ...original, name: 'Submitted A' }], offline: [] },
				digest: 'd2'
			});
		});

		expect((input as HTMLInputElement).value).toBe('New unsaved B');
		expect(
			button(t('common.save')).disabled,
			'save must remain enabled for newer unsaved edits'
		).toBe(false);
	});

	it('keeps the local article draft when synchronizing after 412', async () => {
		vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
		vi.mocked(api.getPost)
			.mockResolvedValueOnce({ post, etag: '"d1"' })
			.mockResolvedValueOnce({ post: { ...post, title: 'Server V2' }, etag: '"d2"' });
		vi.mocked(api.putPost).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'conflict', null)
		);

		mount(<PostsPage />, 'posts');
		await screen.findByText('Original title');
		fireEvent.click(button(t('posts.edit')));

		const title = await screen.findByDisplayValue('Original title');
		fireEvent.change(title, { target: { value: 'Local unsaved title' } });
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(api.putPost).toHaveBeenCalledTimes(1));

		await waitFor(() => expect(button(t('posts.conflictAction'))).toBeTruthy());
		fireEvent.click(button(t('posts.conflictAction')));
		await waitFor(() => expect(api.getPost).toHaveBeenCalledTimes(2));
		await act(async () => {});

		expect((title as HTMLInputElement).value, 'synchronization must preserve the dirty title').toBe(
			'Local unsaved title'
		);
	});

	it('keeps the posts editor read-only for viewers', async () => {
		vi.mocked(api.getSite).mockResolvedValue({ ...site, role: 'viewer' });
		vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
		vi.mocked(api.getPost).mockResolvedValue({ post, etag: '"d1"' });

		mount(<PostsPage />, 'posts');
		await screen.findByText('Original title');
		fireEvent.click(button(t('posts.edit')));

		const title = await screen.findByDisplayValue('Original title');
		await waitFor(() => expect((title as HTMLInputElement).disabled).toBe(true));
		expect(button(t('common.save')).disabled).toBe(true);
	});

	it('drops late document responses after the page unmounts', async () => {
		let resolveA: (x: unknown) => void = () => {};
		vi.mocked(api.getFriends)
			.mockImplementationOnce(
				() =>
					new Promise((resolve) => {
						resolveA = resolve as (x: unknown) => void;
					})
			)
			.mockResolvedValue({
				friends: { online: [{ name: 'Site B friend', url: 'https://b.test' }], offline: [] },
				digest: 'db'
			});

		const { unmount } = render(
			<MemoryRouter initialEntries={[`/sites/${site.siteId}/friends`]}>
				<Routes>
					<Route path="/sites/:siteId/friends" element={<FriendsPage />} />
				</Routes>
			</MemoryRouter>
		);
		// The user leaves the page (site switch / unmount) before the slow
		// response for the old site arrives.
		unmount();
		await act(async () => {
			resolveA({
				friends: { online: [{ name: 'Stale site A friend', url: 'https://a.test' }], offline: [] },
				digest: 'da'
			});
		});
		await act(async () => {
			await Promise.resolve();
		});
		// The stale response must not throw into an unmounted tree or leak
		// into the next mount; render fresh and see only the new data.
		mount(<FriendsPage />, 'friends');
		await screen.findByDisplayValue('Site B friend');
		expect(screen.queryByDisplayValue('Stale site A friend')).toBeNull();
	});
});
