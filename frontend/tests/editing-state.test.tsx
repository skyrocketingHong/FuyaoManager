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
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { render, screen, fireEvent, waitFor, cleanup, act, within } from '@testing-library/react';
import FriendsPage from '../src/pages/content/FriendsPage';
import PostsPage from '../src/pages/content/posts/PostsPage';
import PostEditorPage from '../src/pages/content/posts/PostEditorPage';
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
	getPostTaxonomy: vi.fn(),
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

let reduceMotion = false;
beforeAll(() =>
	Object.defineProperty(window, 'matchMedia', {
		value: (query: string) => ({
			matches: reduceMotion && query === '(prefers-reduced-motion: reduce)',
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
	reduceMotion = false;
	vi.clearAllMocks();
	const NativeRequest = globalThis.Request;
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init ? { ...init, signal: undefined } : init);
			}
		}
	);
	vi.mocked(api.getSite).mockResolvedValue(site);
	vi.mocked(api.getPostTaxonomy).mockResolvedValue({
		directories: [],
		categories: [],
		tags: [],
		postCount: 0,
		canEdit: true
	});
	initLocale();
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

async function loadedArticleTitle(): Promise<HTMLInputElement> {
	const input = (await screen.findByLabelText(t('posts.fieldTitle'))) as HTMLInputElement;
	await waitFor(() => expect(input.value).toBe('Original title'));
	return input;
}

function mount(element: React.ReactElement, section: string, siteId = site.siteId) {
	const router = createMemoryRouter(
		[
			{ path: '/sites/:siteId/' + section, element },
			{ path: '/sites/:siteId/posts/:postPath/edit', element: <PostEditorPage mode="edit" /> }
		],
		{ initialEntries: [`/sites/${siteId}/${section}`] }
	);
	render(<RouterProvider router={router} />);
}

async function mountReorderableFriends() {
	const friends = {
		online: ['Alpha', 'Bravo', 'Charlie'].map((name) => ({
			name,
			url: `https://${name.toLowerCase()}.test`
		})),
		offline: [{ name: 'Delta', url: 'https://delta.test' }]
	};
	vi.mocked(api.getFriends).mockResolvedValue({ friends, digest: 'friends-v1' });
	mount(<FriendsPage />, 'friends');
	const handle = await screen.findByRole('button', {
		name: t('friends.reorderHandle', { name: 'Alpha' })
	});
	const online = within(screen.getByRole('list', { name: t('friends.online') }));
	const items = online.getAllByRole('listitem');
	items.forEach((item, index) => {
		item.getBoundingClientRect = () => new DOMRect(0, index * 48, 500, 40);
	});
	return { friends, handle, online, items };
}

describe('editing state (R5)', () => {
	it('previews keyboard sorting, cancels with Escape, and saves the final group order once', async () => {
		reduceMotion = true;
		const { friends, handle, online, items } = await mountReorderableFriends();
		const [alpha, bravo, charlie] = items;
		const animate = vi.fn();
		alpha.animate = animate;
		handle.focus();
		fireEvent.keyDown(handle, { key: ' ' });
		expect(handle.getAttribute('aria-pressed')).toBe('true');
		expect(
			screen.getByText(t('friends.reorderPicked', { name: 'Alpha', position: 1, total: 3 }))
		).toBeTruthy();
		fireEvent.keyDown(handle, { key: 'ArrowDown' });
		expect(bravo.style.translate).toBe('0px -48px');
		expect(online.getAllByRole('listitem')).toEqual(items);
		expect(button(t('common.save')).disabled).toBe(true);
		fireEvent.keyDown(handle, { key: 'Escape' });
		expect(bravo.style.translate).toBe('');
		expect(handle.getAttribute('aria-pressed')).toBe('false');
		expect(screen.getByText(t('friends.reorderCancelled', { name: 'Alpha' }))).toBeTruthy();

		fireEvent.keyDown(handle, { key: 'Enter' });
		fireEvent.keyDown(handle, { key: 'ArrowDown' });
		fireEvent.keyDown(handle, { key: 'ArrowDown' });
		expect(
			screen.getByText(t('friends.reorderMoved', { name: 'Alpha', position: 3, total: 3 }))
		).toBeTruthy();
		fireEvent.keyDown(handle, { key: 'Enter' });
		expect(online.getAllByRole('listitem')).toEqual([bravo, charlie, alpha]);
		expect(document.activeElement).toBe(handle);
		expect(items.every((item) => item.style.translate === '')).toBe(true);
		expect(animate).not.toHaveBeenCalled();
		expect(vi.mocked(api.putFriends)).not.toHaveBeenCalled();

		const reordered = {
			...friends,
			online: [friends.online[1], friends.online[2], friends.online[0]]
		};
		vi.mocked(api.putFriends).mockResolvedValue({ friends: reordered, digest: 'friends-v2' });
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(button(t('common.save')).disabled).toBe(true));
		expect(api.putFriends).toHaveBeenCalledExactlyOnceWith(site.siteId, reordered, 'friends-v1');
		expect(online.getAllByRole('listitem')).toEqual([bravo, charlie, alpha]);
	});

	it('moves only transforms during pointer drag and commits when landing finishes', async () => {
		const { handle, online, items } = await mountReorderableFriends();
		const [alpha, bravo, charlie] = items;
		let finishLanding = () => {};
		const finished = new Promise<void>((resolve) => {
			finishLanding = resolve;
		});
		const animate = vi.fn(() => ({ finished, cancel: vi.fn() }));
		Object.assign(alpha, { animate });
		const capture = new Set<number>();
		Object.assign(handle, {
			setPointerCapture: (id: number) => capture.add(id),
			hasPointerCapture: (id: number) => capture.has(id),
			releasePointerCapture: (id: number) => capture.delete(id)
		});
		const pointer = { pointerId: 7, isPrimary: true, button: 0, pointerType: 'touch' };
		fireEvent.pointerDown(handle, { ...pointer, clientY: 20 });
		expect(capture.has(7)).toBe(true);
		fireEvent.pointerMove(handle, { ...pointer, clientY: 100 });
		expect(alpha.style.translate).toBe('0px 80px');
		expect(bravo.style.translate).toBe('0px -48px');
		expect(charlie.style.translate).toBe('0px -48px');
		fireEvent.pointerUp(handle, { ...pointer, pointerId: 8, clientY: 100 });
		expect(animate).not.toHaveBeenCalled();
		fireEvent.pointerCancel(handle, pointer);
		expect(capture.has(7)).toBe(false);
		expect(online.getAllByRole('listitem')).toEqual(items);

		fireEvent.pointerDown(handle, { ...pointer, clientY: 20 });
		fireEvent.pointerUp(handle, { ...pointer, clientY: 100 });
		expect(animate).toHaveBeenCalledExactlyOnceWith(
			[
				{ translate: '0px 80px', scale: 1.04 },
				{ translate: '0px 96px', scale: 1 }
			],
			{ duration: 200, easing: 'cubic-bezier(0.2, 0, 0, 1)', fill: 'forwards' }
		);
		fireEvent.lostPointerCapture(handle, pointer);
		expect(online.getAllByRole('listitem')).toEqual(items);
		expect(button(t('common.save')).disabled).toBe(true);
		await act(async () => finishLanding());
		expect(online.getAllByRole('listitem')).toEqual([bravo, charlie, alpha]);
		expect(items.every((item) => item.style.translate === '')).toBe(true);
		expect(button(t('common.save')).disabled).toBe(false);
		expect(
			screen.getByText(t('friends.reorderDropped', { name: 'Alpha', position: 3, total: 3 }))
		).toBeTruthy();
	});

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

		const title = await loadedArticleTitle();
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

	it('keeps fields edited while the conflict synchronization is in flight', async () => {
		let finish!: (result: { post: typeof post & { description: string }; etag: string }) => void;
		vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
		vi.mocked(api.getPost)
			.mockResolvedValueOnce({ post, etag: '"d1"' })
			.mockImplementationOnce(
				() =>
					new Promise((resolve) => {
						finish = resolve;
					})
			);
		vi.mocked(api.putPost).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'conflict', null)
		);
		mount(<PostsPage />, 'posts');
		await screen.findByText('Original title');
		fireEvent.click(button(t('posts.edit')));
		fireEvent.change(await loadedArticleTitle(), {
			target: { value: 'Local title' }
		});
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(button(t('posts.conflictAction'))).toBeTruthy());
		fireEvent.click(button(t('posts.conflictAction')));
		await waitFor(() => expect(api.getPost).toHaveBeenCalledTimes(2));
		const description = screen.getByLabelText(t('posts.fieldDescription')) as HTMLTextAreaElement;
		fireEvent.change(description, { target: { value: 'Typed during sync' } });
		await act(async () =>
			finish({
				post: { ...post, title: 'Server title', description: 'Server description' },
				etag: '"d2"'
			})
		);
		await waitFor(() => expect(description.value).toBe('Typed during sync'));
		expect((screen.getByLabelText(t('posts.fieldTitle')) as HTMLInputElement).value).toBe(
			'Local title'
		);
	});

	it('loads the current server version when discarding edits after a conflict', async () => {
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
		fireEvent.change(await loadedArticleTitle(), {
			target: { value: 'Discard me' }
		});
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(button(t('posts.discardLocal'))).toBeTruthy());
		fireEvent.click(button(t('posts.discardLocal')));
		await screen.findByText(t('posts.discardConfirm'));
		const confirm = document.querySelector(
			'.ant-popconfirm-buttons .ant-btn-primary'
		) as HTMLButtonElement;
		fireEvent.click(confirm);
		await waitFor(() =>
			expect((screen.getByLabelText(t('posts.fieldTitle')) as HTMLInputElement).value).toBe(
				'Server V2'
			)
		);
		expect(button(t('common.save')).disabled).toBe(true);
	});

	it('preserves new input made while the discarded version is being reloaded', async () => {
		let finish!: (result: { post: typeof post & { description: string }; etag: string }) => void;
		vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
		vi.mocked(api.getPost)
			.mockResolvedValueOnce({ post, etag: '"d1"' })
			.mockImplementationOnce(
				() =>
					new Promise((resolve) => {
						finish = resolve;
					})
			);
		vi.mocked(api.putPost).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'conflict', null)
		);
		mount(<PostsPage />, 'posts');
		await screen.findByText('Original title');
		fireEvent.click(button(t('posts.edit')));
		fireEvent.change(await loadedArticleTitle(), {
			target: { value: 'Discard me' }
		});
		fireEvent.click(button(t('common.save')));
		await waitFor(() => expect(button(t('posts.discardLocal'))).toBeTruthy());
		fireEvent.click(button(t('posts.discardLocal')));
		await screen.findByText(t('posts.discardConfirm'));
		fireEvent.click(
			document.querySelector('.ant-popconfirm-buttons .ant-btn-primary') as HTMLButtonElement
		);
		await waitFor(() => expect(api.getPost).toHaveBeenCalledTimes(2));
		const description = screen.getByLabelText(t('posts.fieldDescription')) as HTMLTextAreaElement;
		fireEvent.change(description, { target: { value: 'Newer edit' } });
		await act(async () =>
			finish({
				post: { ...post, title: 'Server V2', description: 'Server description' },
				etag: '"d2"'
			})
		);
		await waitFor(() =>
			expect((screen.getByLabelText(t('posts.fieldTitle')) as HTMLInputElement).value).toBe(
				'Server V2'
			)
		);
		expect(description.value).toBe('Newer edit');
		expect(button(t('common.save')).disabled).toBe(false);
	});

	it('keeps the posts editor read-only for viewers', async () => {
		vi.mocked(api.getSite).mockResolvedValue({ ...site, role: 'viewer' });
		vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
		vi.mocked(api.getPost).mockResolvedValue({ post, etag: '"d1"' });

		mount(<PostsPage />, 'posts');
		await screen.findByText('Original title');
		fireEvent.click(button(t('posts.edit')));

		const title = await loadedArticleTitle();
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
			<RouterProvider
				router={createMemoryRouter([{ path: '/sites/:siteId/friends', element: <FriendsPage /> }], {
					initialEntries: [`/sites/${site.siteId}/friends`]
				})}
			/>
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
