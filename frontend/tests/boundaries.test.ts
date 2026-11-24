/**
 * Source-level boundaries for the frontend layering (the robot platform keeps
 * the same kind of guard next to each library):
 *
 * - Imports only point down: lib ← components ← features ← pages, with the
 *   entry files (App.tsx, main.tsx) free to import anything. A page is the
 *   only layer that may compose other pages, so page-level compositors live
 *   under pages/.
 * - The route table's page registry (pageByNavId in App.tsx) and the
 *   navigation metadata (navItems) are maintained by hand in parallel; the
 *   registry must cover every navigation item exactly once.
 *
 * Both checks read the sources as text through import.meta.glob, so they run
 * in the plain node environment without mounting any component.
 */
import { describe, expect, it } from 'vitest';
import { navItems } from '../src/lib/navigation';

const sources = import.meta.glob('../src/**/*.{ts,tsx}', {
	query: '?raw',
	import: 'default',
	eager: true
}) as Record<string, string>;

// Keys look like '../src/components/layout/TopBar.tsx'.
function layer(key: string): string {
	const segment = key.split('/')[2];
	return /\.(ts|tsx)$/.test(segment) ? 'entry' : segment;
}

function resolveSpecifier(
	all: Record<string, string>,
	from: string,
	spec: string
): string | null {
	const parts = from.split('/').slice(0, -1);
	for (const part of spec.split('/')) {
		if (part === '.') continue;
		if (part === '..') parts.pop();
		else parts.push(part);
	}
	const target = parts.join('/');
	for (const suffix of ['', '.ts', '.tsx', '/index.ts', '/index.tsx', '.json', '.css']) {
		if (target + suffix in all) return target + suffix;
	}
	return null;
}

function relativeSpecifiers(source: string): string[] {
	const specs: string[] = [];
	const pattern =
		/\bfrom\s*['"]([^'"]+)['"]|\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)|\bimport\s+['"]([^'"]+)['"]/g;
	for (const match of source.matchAll(pattern)) {
		const spec = match[1] ?? match[2] ?? match[3];
		if (spec?.startsWith('.')) specs.push(spec);
	}
	return specs;
}

const allowedTargets: Record<string, string[]> = {
	lib: ['lib'],
	components: ['components', 'lib'],
	features: ['features', 'components', 'lib'],
	pages: ['pages', 'features', 'components', 'lib'],
	entry: ['entry', 'pages', 'features', 'components', 'lib']
};

function findViolations(all: Record<string, string>): string[] {
	const violations: string[] = [];
	for (const [key, source] of Object.entries(all)) {
		const from = layer(key);
		if (!allowedTargets[from]) continue;
		for (const spec of relativeSpecifiers(source)) {
			const target = resolveSpecifier(all, key, spec);
			if (target && !allowedTargets[from].includes(layer(target)))
				violations.push(`${key.slice(3)} imports ${spec}`);
		}
	}
	return violations;
}

describe('layer boundaries', () => {
	it('keeps every relative import pointing at the same or a lower layer', () => {
		expect(findViolations(sources)).toEqual([]);
	});

	it('reports an import that points up the layering', () => {
		// A self-check for the checker: the same analysis on a small tree with
		// one upward import (lib → components) has to report exactly that.
		expect(
			findViolations({
				'../src/lib/a.ts': "import { b } from '../components/B';\nimport { c } from './c';",
				'../src/lib/c.ts': 'export const c = 1;',
				'../src/components/B.tsx': "export const b = 1;\nexport { d } from '../../lib/d';",
				'../src/lib/d.ts': 'export const d = 1;'
			})
		).toEqual(['src/lib/a.ts imports ../components/B']);
	});

	it('resolves specifiers the way the bundler does', () => {
		expect(layer('../src/lib/navigation/index.ts')).toBe('lib');
		expect(layer('../src/App.tsx')).toBe('entry');
		expect(
			resolveSpecifier(sources, '../src/components/layout/TopBar.tsx', '../../lib/i18n')
		).toBe('../src/lib/i18n/index.ts');
		expect(
			resolveSpecifier(sources, '../src/pages/content/PostWorkspace.tsx', './posts/PostsPage')
		).toBe('../src/pages/content/posts/PostsPage.tsx');
		expect(
			relativeSpecifiers(
				"import a from './a';\nexport { b } from '../b';\nconst c = () => import('../c');\nimport './d.css';"
			)
		).toEqual(['./a', '../b', '../c', './d.css']);
	});
});

describe('page registry', () => {
	it('registers exactly one page component for every navigation item', () => {
		const app = sources['../src/App.tsx'];
		const start = app.indexOf('const pageByNavId');
		expect(start).toBeGreaterThan(-1);
		const table = app.slice(start, app.indexOf('\n};', start));
		// One key per line, quoted or bare, at the table's own indentation.
		const keys = [...table.matchAll(/^\t(?:'([^']+)'|([\w.]+)):\s/gm)].map((match) => match[1] ?? match[2]);
		const expected = navItems.map((item) => item.id);
		expect([...new Set(keys)].sort()).toEqual([...new Set(expected)].sort());
	});
});
