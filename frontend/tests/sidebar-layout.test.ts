/**
 * Sidebar layout guards. jsdom lays nothing out, so the sidebar's two
 * invariants are checked on the stylesheet text instead:
 *
 * - One icon axis: the mode control, the fixed actions, the menu entries and
 *   the footer all center their icon 26px from the left edge, which is the
 *   middle of the 52px icon rail. The rail is therefore the expanded sidebar
 *   cut down to its first 52px.
 * - No vertical change between modes: the icon-rail rules only hide labels
 *   and center group titles. They declare no height, no block padding or
 *   margin and no line box of their own, and nothing outside the rail block
 *   restyles the sidebar's regions for the icon mode. An entry keeps its
 *   vertical position when the sidebar narrows, when the full menu floats
 *   out and when the mode changes.
 *
 * The metrics themselves follow the robot platform's sidebar: 36px rows
 * (44px for fingers), a 24px icon slot with a 20px glyph, a fixed height for
 * group titles and a 76px brand row.
 */
import { describe, expect, it } from 'vitest';
import sidebarCss from '../src/styles/sidebar/sidebar.css?raw';
import baseCss from '../src/styles/base.css?raw';

const withoutComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '');

type Declaration = { property: string; value: string };
type Rule = { selector: string; declarations: Declaration[] };

function rules(css: string): Rule[] {
	return [...withoutComments(css).matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((match) => ({
		selector: match[1].trim().replace(/\s+/g, ' '),
		declarations: match[2]
			.split(';')
			.map((declaration) => declaration.trim())
			.filter(Boolean)
			.map((declaration) => ({
				property: declaration.slice(0, declaration.indexOf(':')).trim(),
				value: declaration.slice(declaration.indexOf(':') + 1).trim().replace(/\s+/g, ' ')
			}))
	}));
}

const valueOf = (rule: Rule | undefined, property: string) =>
	rule?.declarations.find((declaration) => declaration.property === property)?.value;

const px = (value: string | undefined) => {
	const match = /^(\d+(?:\.\d+)?)px$/.exec(value ?? '');
	if (!match) throw new Error(`not a pixel value: ${value}`);
	return Number(match[1]);
};

const railQuery = '@container sidebar (width < 156px)';

/** The icon-rail block and the stylesheet without it. */
function splitRailBlock(css: string) {
	const text = withoutComments(css);
	const open = text.indexOf(railQuery);
	if (open === -1) throw new Error('the stylesheet has no icon-rail block');
	const start = text.indexOf('{', open);
	let depth = 1;
	let end = start + 1;
	for (; end < text.length && depth > 0; end++) {
		if (text[end] === '{') depth++;
		else if (text[end] === '}') depth--;
	}
	return { block: text.slice(start + 1, end - 1), rest: text.slice(0, open) + text.slice(end) };
}

const sidebar = rules(sidebarCss);
const space1 = px(/--space-1:\s*(\d+px)/.exec(baseCss)?.[1]);
const railWidth = px(
	sidebar
		.filter((rule) => rule.selector === '.app-sider')
		.map((rule) => valueOf(rule, '--sidebar-rail-width'))
		.find(Boolean)
);
const content = sidebar.find((rule) => rule.selector === '.sidebar-content');
const coarse = withoutComments(sidebarCss).match(/@media \(pointer: coarse\)\s*\{\s*\.sidebar-content\s*\{([^}]*)\}/)?.[1];

describe('sidebar metrics', () => {
	it('follows the robot platform: rows, icon slot, glyph, group title and brand', () => {
		expect(valueOf(content, '--sidebar-row-height')).toBe('36px');
		expect(valueOf(content, '--sidebar-icon-size')).toBe('24px');
		expect(valueOf(content, '--sidebar-group-height')).toBe('36px');
		expect(coarse).toMatch(/--sidebar-row-height:\s*44px/);
		const glyph = sidebar.find((rule) => rule.selector.includes('.ant-menu-item-icon > svg[viewBox]'));
		expect(valueOf(glyph, 'width')).toBe('20px');
		expect(valueOf(glyph, 'height')).toBe('20px');
		expect(valueOf(sidebar.find((rule) => rule.selector === '.sidebar-brand'), 'height')).toBe('76px');
	});

	it('gives a group title one height in every state', () => {
		const title = sidebar.find((rule) => rule.selector.endsWith('.ant-menu-item-group-title'));
		expect(valueOf(title, 'block-size')).toBe('var(--sidebar-group-height)');
		expect(valueOf(title, 'padding-block')).toBe('0');
		expect(valueOf(title, 'margin')).toBe('0');
	});

	it('centers every icon on the middle of the icon rail', () => {
		const gutter = px(valueOf(content, '--sidebar-gutter'));
		const icon = px(valueOf(content, '--sidebar-icon-size'));
		expect(space1 + gutter + icon / 2).toBe(railWidth / 2);

		// The menu: the region's inset, then the entry's.
		const nav = sidebar.find((rule) => rule.selector === '.sidebar-content nav');
		expect(valueOf(nav, 'padding-inline')).toBe('var(--space-1)');
		const item = sidebar.find((rule) => rule.selector === '.sidebar-content .ant-menu-item');
		expect(valueOf(item, 'padding-inline')).toBe('var(--sidebar-gutter) !important');
		// The fixed actions: the region's inset, then the button's.
		const actions = sidebar.find((rule) => rule.selector === '.sidebar-actions');
		expect(valueOf(actions, 'padding')).toBe('0 var(--space-1) var(--space-2)');
		const action = sidebar.find((rule) => rule.selector === '.sidebar-actions .ant-btn');
		expect(valueOf(action, 'padding-inline')).toBe('var(--sidebar-gutter)');
		// The footer: its margin stands in for the region's inset.
		const foot = sidebar.find((rule) => rule.selector === '.sidebar-foot');
		expect(valueOf(foot, 'margin')).toBe('var(--space-3) var(--space-1) var(--space-4)');
		expect(valueOf(foot, 'padding')).toBe('var(--space-2) var(--sidebar-gutter) 0');
		// The brand control: centered in a column as wide as the rail.
		const brand = sidebar.find((rule) => rule.selector === '.sidebar-brand:has(> .sidebar-mode-button)');
		expect(valueOf(brand, 'padding-inline-start')).toBe(
			'calc((var(--sidebar-rail-width, 52px) - var(--sidebar-mode-size)) / 2)'
		);
	});

	it('gives the location the expanded width so a name breaks the same way in the rail', () => {
		const location = sidebar.find((rule) => rule.selector === '.app-sider .sidebar-location');
		expect(valueOf(location, 'inline-size')).toBe(
			'calc(var(--sidebar-full-width) - 2 * var(--sidebar-content-inset))'
		);
	});
});

describe('icon rail', () => {
	const { block, rest } = splitRailBlock(sidebarCss);
	const railRules = rules(block);

	// What the rail may do: take labels out of the row, move things
	// sideways, wrap the group titles and drop the scrollbar. Nothing here
	// changes a height or a vertical position.
	const horizontalOnly = new Set([
		'padding-inline',
		'margin-inline',
		'text-align',
		'overflow',
		'overflow-wrap',
		'text-wrap',
		'hyphens',
		'white-space',
		'-webkit-box-orient',
		'-webkit-line-clamp',
		'scrollbar-width',
		'scrollbar-gutter'
	]);

	it('hides labels and centers group titles without changing any vertical metric', () => {
		expect(railRules.length).toBeGreaterThan(4);
		const offenders: string[] = [];
		for (const rule of railRules) {
			for (const { property, value } of rule.declarations) {
				const allowed =
					horizontalOnly.has(property) ||
					(property === 'visibility' && value === 'hidden') ||
					(property === 'display' && (value === 'none' || value === '-webkit-box')) ||
					// The clamped title text has its own line box inside the fixed title height.
					(property === 'line-height' && rule.selector.includes('.sidebar-group-text'));
				if (!allowed) offenders.push(`${rule.selector} { ${property}: ${value} }`);
			}
		}
		expect(offenders).toEqual([]);
	});

	it('labels every rule with the icon mode, so the expanded sidebar is untouched', () => {
		expect(railRules.map((rule) => rule.selector.startsWith(".app-sider[data-mode='icons']"))).toEqual(
			railRules.map(() => true)
		);
	});

	it('keeps the scope name and the brand in the rail instead of removing them', () => {
		const kept = railRules.find(
			(rule) =>
				rule.selector ===
				".app-sider[data-mode='icons'] .sidebar-brand > a, .app-sider[data-mode='icons'] .sidebar-location"
		);
		expect(kept?.declarations).toEqual([{ property: 'visibility', value: 'hidden' }]);
		// Labels are the only things taken out of the row, and they leave in
		// one rule: the menu entries, the fixed actions' text and trailing
		// icons, and the footer's version.
		const hidden = railRules.filter((rule) =>
			rule.declarations.some(({ property, value }) => property === 'display' && value === 'none')
		);
		expect(hidden.map((rule) => rule.selector)).toEqual([
			".app-sider[data-mode='icons'] :is(.ant-menu-title-content, .sidebar-action-label, .sidebar-action-trailing, .sidebar-foot-version)"
		]);
	});

	// Outside the rail block, the icon mode may only size and shade the
	// floating surface and turn focus rings inward.
	const surfaceRules = [
		/^\.app-sider\[data-mode='icons'\]$/,
		/^\.app-sider\[data-mode='icons'\] > \.ant-layout-sider-children$/,
		/^\.app-sider\[data-mode='icons'\] \.sidebar-panel$/,
		/^\.app-sider\[data-mode='icons'\] :is\(\.sidebar-actions \.ant-btn, \.ant-menu-item, \.sidebar-foot-link\):focus-visible$/,
		/^\.app-sider\[data-mode='icons'\]:not\(\[data-peek-dismissed\]\):has\(:focus-visible\)$/,
		/^\.app-sider\[data-mode='icons'\]:not\(\[data-peek-dismissed\]\):hover$/,
		/^\.app-sider\[data-mode='icons'\] > \.ant-layout-sider-children, \.app-sider\[data-mode='icons'\] \.sidebar-panel \*$/
	];

	it('restyles no sidebar region for the icon mode outside the rail block', () => {
		const others = rules(rest).filter((rule) => rule.selector.includes("[data-mode='icons']"));
		expect(others).toHaveLength(surfaceRules.length);
		expect(
			others.map((rule) => rule.selector).filter((selector) => !surfaceRules.some((pattern) => pattern.test(selector)))
		).toEqual([]);
	});

	it('catches a vertical override added to the rail block', () => {
		const withJump = sidebarCss.replace(
			railQuery + ' {',
			`${railQuery} {\n\t.app-sider[data-mode='icons'] .sidebar-brand { height: 56px; }\n\t.app-sider[data-mode='icons'] .sidebar-foot { padding-block: 0; display: flex; }`
		);
		const added = rules(splitRailBlock(withJump).block).filter((rule) =>
			rule.declarations.some(({ property }) => property === 'height' || property === 'padding-block')
		);
		expect(added.map((rule) => rule.selector)).toEqual([
			".app-sider[data-mode='icons'] .sidebar-brand",
			".app-sider[data-mode='icons'] .sidebar-foot"
		]);
	});
});
