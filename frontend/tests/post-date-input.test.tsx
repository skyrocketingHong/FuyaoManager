// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import PostDateInput, { isValidPostDate } from '../src/components/PostDateInput';
import { setLocale } from '../src/lib/i18n';

afterEach(cleanup);

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

	it('keeps the original time and offset when a date is changed', async () => {
		setLocale('en-US');
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
		await waitFor(() => expect(change).toHaveBeenCalledWith('2026-11-13T10:00:45.123+08:00'));
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
});
