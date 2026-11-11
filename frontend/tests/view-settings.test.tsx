/**
 * Reactive-view verification: mounts the real SettingsPage in jsdom (no
 * browser) and asserts that UI derivations — the save button state and the
 * status notices — actually update through React re-rendering when the
 * editor's baseline or form changes.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import type { Site, SiteSettings } from '../src/lib/api/management';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		getSettings: vi.fn(),
		updateSettings: vi.fn()
	};
});

import { getSite, getSettings, updateSettings } from '../src/lib/api/management';
import SettingsPage from '../src/pages/sites/SettingsPage';
import { setLocale } from '../src/lib/i18n';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const baseSite: Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 3,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

const baseSettings: SiteSettings = {
	siteId: baseSite.siteId,
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	revision: 3,
	updatedAt: '2026-01-01T00:00:00Z'
};

// antd requires matchMedia in jsdom.
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

async function settled(): Promise<void> {
	for (let i = 0; i < 12; i++) await Promise.resolve();
}

/** antd inserts a space between two-CJK-character button labels ("保 存"). */
function findSaveButton(): HTMLButtonElement {
	const button = screen
		.getAllByRole('button')
		.find((b) => (b.textContent ?? '').replace(/\s/g, '') === saveLabelText());
	if (!(button instanceof HTMLButtonElement)) throw new Error('save button missing');
	return button;
}

function saveLabelText(): string {
	return zh['common.save'] as string;
}

function mountView() {
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[`/sites/${baseSite.siteId}/settings`]}>
				<Routes>
					<Route path="/sites/:siteId/settings" element={<SettingsPage />} />
					<Route path="/login" element={<div>login</div>} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(getSettings).mockReset();
	vi.mocked(updateSettings).mockReset();
	localStorage.clear();
	setLocale('zh-CN');
});

describe('SettingsPage reactive derivations', () => {
	it('disables the save button while clean and enables it on edits', async () => {
		vi.mocked(getSite).mockResolvedValue(baseSite);
		vi.mocked(getSettings).mockResolvedValue({ settings: baseSettings, etag: '"e-r3"' });
		mountView();
		// Wait for the form to load before deriving button state.
		const input = await screen.findByDisplayValue('Baseline Name');
		await settled();

		await waitFor(() => {
			const button = findSaveButton();
			expect(button).toBeDefined();
			expect((button as HTMLButtonElement).disabled).toBe(true);
		});

		await act(async () => {
			fireEvent.change(input, { target: { value: 'Edited Name' } });
		});
		await settled();
		await waitFor(() => {
			const button = findSaveButton();
			expect((button as HTMLButtonElement).disabled).toBe(false);
		});
	});

	it('shows the saved notice after a successful save and re-disables the button', async () => {
		vi.mocked(getSite).mockResolvedValue(baseSite);
		vi.mocked(getSettings).mockResolvedValue({ settings: baseSettings, etag: '"e-r3"' });
		vi.mocked(updateSettings).mockResolvedValue({
			settings: { ...baseSettings, name: 'Edited Name', revision: 4 },
			etag: '"e-r4"'
		});
		mountView();
		const input = await screen.findByDisplayValue('Baseline Name');
		await act(async () => {
			fireEvent.change(input, { target: { value: 'Edited Name' } });
		});
		await settled();

		await waitFor(() => {
			const button = findSaveButton();
			expect((button as HTMLButtonElement).disabled).toBe(false);
		});
		await act(async () => {
			fireEvent.click(findSaveButton());
		});
		await settled();

		expect(updateSettings).toHaveBeenCalledWith(
			baseSite.siteId,
			{ name: 'Edited Name' },
			'"e-r3"'
		);
		const savedNotice = (zh['settings.savedAt'] as string).replace('{revision}', '4');
		await waitFor(() => {
			expect(screen.getByText(savedNotice)).toBeDefined();
		});
		await waitFor(() => {
			const after = findSaveButton();
			expect((after as HTMLButtonElement).disabled).toBe(true);
		});
	});

	it('renders the viewer notice and disables inputs for read-only roles', async () => {
		vi.mocked(getSite).mockResolvedValue({ ...baseSite, role: 'viewer' });
		vi.mocked(getSettings).mockResolvedValue({ settings: baseSettings, etag: '"e-r3"' });
		mountView();
		const notice = zh['settings.viewerNotice'] as string;
		await waitFor(() => {
			expect(screen.getByText(notice)).toBeDefined();
		});
	});
});
