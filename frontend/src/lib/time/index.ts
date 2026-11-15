import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';

dayjs.extend(utc);
dayjs.extend(timezone);

export interface TimePreferences {
	timezone: string;
	preset: 'iso' | 'readable' | 'custom';
	dateFormat: string;
	dateTimeFormat: string;
}

export const timeStorageKey = 'fuyao-manager-time:v1';
export const defaultTimePreferences: TimePreferences = {
	timezone: 'system',
	preset: 'iso',
	dateFormat: 'YYYY-MM-DD',
	dateTimeFormat: 'YYYY-MM-DDTHH:mm:ssZ'
};

function validZone(value: string): boolean {
	if (value === 'system') return true;
	try {
		new Intl.DateTimeFormat('en', { timeZone: value });
		return true;
	} catch {
		return false;
	}
}

function normalized(value: unknown): TimePreferences {
	const input = value && typeof value === 'object' ? (value as Partial<TimePreferences>) : {};
	const format = (value: unknown, fallback: string) =>
		typeof value === 'string' && value.trim() && value.length <= 100 ? value : fallback;
	return {
		timezone:
			typeof input.timezone === 'string' && validZone(input.timezone) ? input.timezone : 'system',
		preset: input.preset === 'readable' || input.preset === 'custom' ? input.preset : 'iso',
		dateFormat: format(input.dateFormat, defaultTimePreferences.dateFormat),
		dateTimeFormat: format(input.dateTimeFormat, defaultTimePreferences.dateTimeFormat)
	};
}

function read(): TimePreferences {
	try {
		return normalized(JSON.parse(localStorage.getItem(timeStorageKey) ?? '{}'));
	} catch {
		return { ...defaultTimePreferences };
	}
}

let preferences = read();
const listeners = new Set<() => void>();
export function timeSnapshot(): TimePreferences {
	return preferences;
}
export function subscribeTime(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}
export function setTimePreferences(patch: Partial<TimePreferences>): void {
	preferences = normalized({ ...preferences, ...patch });
	try {
		localStorage.setItem(timeStorageKey, JSON.stringify(preferences));
	} catch {
		/* Keep the in-memory preference. */
	}
	for (const listener of listeners) listener();
}
export function resetTimePreferences(): void {
	setTimePreferences(defaultTimePreferences);
}
export function initTimePreferences(): () => void {
	preferences = read();
	const onStorage = (event: StorageEvent) => {
		if (event.key !== timeStorageKey && event.key !== null) return;
		preferences = read();
		for (const listener of listeners) listener();
	};
	window.addEventListener('storage', onStorage);
	for (const listener of listeners) listener();
	return () => window.removeEventListener('storage', onStorage);
}

export function displayTimezone(value = preferences.timezone): string {
	if (value !== 'system') return value;
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

export function timezones(): string[] {
	const supported = (Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] })
		.supportedValuesOf;
	try {
		return Array.from(new Set(['UTC', 'Asia/Shanghai', ...(supported?.('timeZone') ?? [])]));
	} catch {
		return ['UTC', 'Asia/Shanghai'];
	}
}

export function validCalendarDate(value: string): boolean {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
	const [year, month, day] = value.split('-').map(Number);
	const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
	const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
	return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1];
}

export function postDateParts(
	value: string
): { date: string; time: string; fraction: string } | null {
	const match =
		/^(\d{4}-\d{2}-\d{2})(?:T(\d{2}:\d{2})(?::(\d{2})(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?)?$/.exec(value);
	if (!match || !validCalendarDate(match[1])) return null;
	if (match[2]) {
		const [hours, minutes] = match[2].split(':').map(Number);
		if (hours > 23 || minutes > 59 || Number(match[3] ?? 0) > 59) return null;
	}
	if (
		match[5] &&
		match[5] !== 'Z' &&
		(Number(match[5].slice(1, 3)) > 23 || Number(match[5].slice(4, 6)) > 59)
	)
		return null;
	return {
		date: match[1],
		time: match[2] ? `${match[2]}${match[3] ? `:${match[3]}` : ''}` : '',
		fraction: match[4] ?? ''
	};
}

export function dateTimeValue(value: string, zone = preferences.timezone) {
	const parts = postDateParts(value);
	if (!parts?.time) return null;
	const normalizedValue = /(?:Z|[+-]\d{2}:\d{2})$/i.test(value) ? value : `${value}Z`;
	const parsed = dayjs(normalizedValue);
	return parsed.isValid() ? parsed.tz(displayTimezone(zone)) : null;
}

export function formatTime(value: string): string {
	if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
		if (!validCalendarDate(value)) return value;
		const [year, month, day] = value.split('-').map(Number);
		const date = new Date(0);
		date.setUTCFullYear(year, month - 1, day);
		date.setUTCHours(12, 0, 0, 0);
		return dayjs.utc(date).format(preferences.dateFormat);
	}
	const date = dateTimeValue(value);
	return date ? date.format(preferences.dateTimeFormat) : value;
}

export function wallTimeToUTC(
	date: string,
	time: string,
	fraction: string,
	zone = preferences.timezone
): string | null {
	const wall = `${date}T${time.length === 5 ? `${time}:00` : time}`;
	try {
		const parsed = dayjs.tz(wall, displayTimezone(zone));
		if (!parsed.isValid() || parsed.format('YYYY-MM-DDTHH:mm:ss') !== wall) return null;
		return `${parsed.utc().format('YYYY-MM-DDTHH:mm:ss')}${fraction}Z`;
	} catch {
		return null;
	}
}
