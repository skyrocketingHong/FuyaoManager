// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import PostDateInput, { isValidPostDate } from '../src/features/posts/PostDateInput';
import { setLocale } from '../src/lib/i18n';
import { resetTimePreferences, setTimePreferences } from '../src/lib/time';

afterEach(() => {
	cleanup();
	resetTimePreferences();
});

describe('post date control', () => {
	it('accepts the content contract and rejects impossible dates and offsets', () => {
		for (const value of [
			'0000-02-29',
			'0099-01-01',
			'2024-02-29',
			'2026-11-12T10:00',
			'2026-11-12T10:00:45.123+08:00',
			'2026-11-12T10:00Z'
		]) {
			expect(isValidPostDate(value), value).toBe(true);
		}
		for (const value of [
			'1900-02-29',
			'2026-02-29',
			'2026-11-12T24:00',
			'2026-11-12T10:00+24:00',
			'2026-11-12T10:00.1Z'
		]) {
			expect(isValidPostDate(value), value).toBe(false);
		}
	});

	it('displays the chosen timezone and writes the changed date in UTC', async () => {
		setLocale('en-US');
		setTimePreferences({ timezone: 'Asia/Shanghai' });
		const change = vi.fn();
		render(
			<ConfigProvider theme={{ token: { motion: false } }}>
				<PostDateInput
					id="post-date"
					label="Published"
					value="2026-11-12T10:00:45.123+08:00"
					onChange={change}
				/>
			</ConfigProvider>
		);
		const input = document.querySelector('#post-date') as HTMLInputElement;
		expect(input.value).toBe('2026-11-12');
		expect(change).not.toHaveBeenCalled();
		fireEvent.change(input, { target: { value: '2026-11-13' } });
		fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
		await waitFor(() => expect(change).toHaveBeenCalledWith('2026-11-13T02:00:45.123Z'));
		expect(screen.getByLabelText('Published Time')).toBeDefined();
	});

	it('displays a four-digit year below 100 without changing it', () => {
		const change = vi.fn();
		render(
			<ConfigProvider>
				<PostDateInput id="ancient-date" label="Published" value="0099-01-01" onChange={change} />
			</ConfigProvider>
		);
		expect((document.querySelector('#ancient-date') as HTMLInputElement).value).toBe('0099-01-01');
		expect(change).not.toHaveBeenCalled();
	});

	it('shows UTC instants in the selected display timezone without rewriting them', () => {
		setTimePreferences({ timezone: 'Asia/Shanghai' });
		const change = vi.fn();
		render(
			<ConfigProvider>
				<PostDateInput
					id="converted-date"
					label="Published"
					value="2026-11-12T20:00:00Z"
					onChange={change}
				/>
			</ConfigProvider>
		);
		expect((document.querySelector('#converted-date') as HTMLInputElement).value).toBe(
			'2026-11-13'
		);
		expect((screen.getByLabelText('Published Time') as HTMLInputElement).value).toBe('04:00:00');
		expect(screen.getByText('UTC+08:00')).toBeDefined();
		expect(change).not.toHaveBeenCalled();
	});
});
