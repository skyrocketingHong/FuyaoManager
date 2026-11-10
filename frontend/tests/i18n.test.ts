import { describe, expect, it } from 'vitest';
import en from '../src/lib/i18n/locales/en-US.json';
import zh from '../src/lib/i18n/locales/zh-CN.json';

describe('i18n dictionaries', () => {
	it('cover the identical key set in both locales', () => {
		expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort());
	});

	it('have no empty values', () => {
		for (const [key, value] of Object.entries(en)) {
			expect(value, `en-US.${key}`).not.toBe('');
		}
		for (const [key, value] of Object.entries(zh)) {
			expect(value, `zh-CN.${key}`).not.toBe('');
		}
	});

	it('keeps placeholder markers consistent between locales', () => {
		const placeholders = (value: string) => (value.match(/\{[^}]+\}/g) ?? []).sort();
		for (const [key, value] of Object.entries(en)) {
			expect(placeholders(zh[key as keyof typeof zh] ?? ''), key).toEqual(placeholders(value));
		}
	});
});
