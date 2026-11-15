// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import DateTimeSettings from '../src/features/settings/DateTimeSettings';
import { setLocale } from '../src/lib/i18n';
import {
	formatTime,
	resetTimePreferences,
	setTimePreferences,
	timeSnapshot,
	timeStorageKey,
	wallTimeToUTC
} from '../src/lib/time';

afterEach(() => {
	cleanup();
	localStorage.removeItem(timeStorageKey);
	resetTimePreferences();
});

it('formats UTC instants in the selected timezone and preserves date-only values', () => {
	setTimePreferences({
		timezone: 'Asia/Shanghai',
		preset: 'custom',
		dateFormat: 'YYYY/MM/DD',
		dateTimeFormat: 'YYYY-MM-DDTHH:mm:ssZ'
	});
	expect(formatTime('2026-11-12T20:15:00Z')).toBe('2026-11-13T04:15:00+08:00');
	expect(formatTime('2026-11-12')).toBe('2026/11/12');
	expect(formatTime('2026-02-30')).toBe('2026-02-30');
	expect(wallTimeToUTC('2026-11-13', '04:15:00', '.123', 'Asia/Shanghai')).toBe(
		'2026-11-12T20:15:00.123Z'
	);
	expect(wallTimeToUTC('2026-03-08', '02:30:00', '', 'America/New_York')).toBeNull();
});

it('saves a custom pattern from the display settings', async () => {
	setLocale('en-US');
	render(
		<ConfigProvider theme={{ token: { motion: false } }}>
			<DateTimeSettings />
		</ConfigProvider>
	);
	fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Date format' }));
	fireEvent.click(
		await screen.findByText('Custom', { selector: '.ant-select-item-option-content' })
	);
	const input = screen.getByLabelText('Date and time pattern');
	fireEvent.change(input, { target: { value: 'YYYY/MM/DD HH:mm Z' } });
	fireEvent.blur(input);
	await waitFor(() => expect(timeSnapshot().dateTimeFormat).toBe('YYYY/MM/DD HH:mm Z'));
	expect(JSON.parse(localStorage.getItem(timeStorageKey)!).preset).toBe('custom');
});
