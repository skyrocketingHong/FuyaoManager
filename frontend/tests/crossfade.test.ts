/**
 * Structural check: <Crossfade> rendered inside phrasing containers
 * (p, h1-h6, span, button, a) must use inline mode, per the project rules —
 * the block mode emits a div, which would be invalid HTML inside those
 * elements and break hydration. The checker walks a tag stack per file so
 * nesting is tracked, not just adjacency.
 */
import { describe, expect, it } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const PHASING = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'span', 'button', 'a', 'label']);

function listSvelteFiles(dir: string): string[] {
	const out: string[] = [];
	for (const entry of readdirSync(dir)) {
		const full = join(dir, entry);
		if (statSync(full).isDirectory()) {
			out.push(...listSvelteFiles(full));
		} else if (entry.endsWith('.svelte')) {
			out.push(full);
		}
	}
	return out;
}

function checkFile(path: string): string[] {
	const violations: string[] = [];
	const lines = readFileSync(path, 'utf8').split('\n');
	const stack: Array<{ tag: string; line: number }> = [];

	for (let i = 0; i < lines.length; i++) {
		const line = lines[i];
		// Track opens of the phrasing containers and Crossfade.
		const open = /<(p|h[1-6]|span|button|a|label|Crossfade)\b([^>]*?)(\/?)>/g;
		for (const match of line.matchAll(open)) {
			if (match[3] === '/') continue; // self-closing: no children
			stack.push({ tag: match[1], line: i + 1 });
		}
		// Crossfade usage check: does the tag carry the inline attribute?
		const crossfade = /<Crossfade\b([^>]*)>/g;
		for (const match of line.matchAll(crossfade)) {
			const attrs = match[1];
			const inline = /\binline\b/.test(attrs);
			if (!inline) {
				const enclosing = [...stack].reverse().find((f) => f.tag !== 'Crossfade');
				if (enclosing && PHASING.has(enclosing.tag)) {
					violations.push(
						`${path}:${i + 1}: <Crossfade> inside <${enclosing.tag}> (line ${enclosing.line}) must use inline mode`
					);
				}
			}
		}
		// Track closes: a close tag pops the nearest matching open.
		const close = /<\/(p|h[1-6]|span|button|a|label|Crossfade)>/g;
		for (const match of line.matchAll(close)) {
			const idx = [...stack].reverse().findIndex((f) => f.tag === match[1]);
			if (idx >= 0) stack.splice(stack.length - 1 - idx, 1);
		}
	}
	return violations;
}

describe('Crossfade inline nesting rule', () => {
	it('has no block Crossfade inside phrasing containers', () => {
		const files = listSvelteFiles(join(import.meta.dirname, '..', 'src'));
		expect(files.length).toBeGreaterThan(0);
		const violations = files.flatMap(checkFile);
		expect(violations).toEqual([]);
	});
});
