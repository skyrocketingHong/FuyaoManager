/**
 * Semantic appearance tokens for the console: the day/night palettes, the
 * color math that derives readable primary ramps from the accent, and the
 * single place that produces both the antd ThemeConfig and the CSS custom
 * properties. Adapted from the reference platform's appearance model with
 * FuyaoManager's own accent and naming.
 */
import { theme as antdTheme } from 'antd';
import type { ThemeConfig } from 'antd';

export type AppearanceMode = 'light' | 'dark';

export interface Palette {
	background: string;
	foreground: string;
	card: string;
	surface: string;
	elevated: string;
	muted: string;
	secondary: string;
	border: string;
	controlBorder: string;
	sidebar: string;
	good: string;
	warning: string;
	danger: string;
	info: string;
}

const ACCENT = '#1677ff';

export const palettes: Record<AppearanceMode, Palette> = {
	light: {
		background: '#f2f5fa',
		foreground: '#1e293b',
		card: '#ffffff',
		surface: '#f6f8fc',
		elevated: '#ffffff',
		muted: '#e9eef5',
		secondary: '#526176',
		border: '#d8e1ee',
		controlBorder: '#7b8a9e',
		sidebar: '#f9fbff',
		good: '#237b4b',
		warning: '#946200',
		danger: '#c63742',
		info: '#1668dc'
	},
	dark: {
		background: '#10151d',
		foreground: '#e7edf5',
		card: '#181f2a',
		surface: '#202938',
		elevated: '#202938',
		muted: '#253143',
		secondary: '#a9b8cc',
		border: '#344155',
		controlBorder: '#71849e',
		sidebar: '#141b25',
		good: '#7bd4a7',
		warning: '#e9bd73',
		danger: '#ff9ca4',
		info: '#80b3ff'
	}
};

// ---------------------------------------------------------------------------
// Color math (sRGB channel mixing + WCAG relative luminance).
// ---------------------------------------------------------------------------

function channels(hex: string): [number, number, number] {
	const value = parseInt(hex.slice(1), 16);
	return [(value >> 16) & 0xff, (value >> 8) & 0xff, value & 0xff];
}

function toHex([r, g, b]: [number, number, number]): string {
	const channel = (n: number) =>
		Math.round(Math.min(255, Math.max(0, n)))
			.toString(16)
			.padStart(2, '0');
	return `#${channel(r)}${channel(g)}${channel(b)}`;
}

export function mixColor(a: string, b: string, ratio: number): string {
	const ca = channels(a);
	const cb = channels(b);
	return toHex([
		ca[0] * (1 - ratio) + cb[0] * ratio,
		ca[1] * (1 - ratio) + cb[1] * ratio,
		ca[2] * (1 - ratio) + cb[2] * ratio
	]);
}

function luminance(hex: string): number {
	const [r, g, b] = channels(hex).map((v) => {
		const c = v / 255;
		return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrastRatio(a: string, b: string): number {
	const la = luminance(a);
	const lb = luminance(b);
	const [hi, lo] = la > lb ? [la, lb] : [lb, la];
	return (hi + 0.05) / (lo + 0.05);
}

/**
 * Finds the closest color to `target` that still reads at `minRatio` against
 * `background`, walking the target toward the background's opposite extreme
 * in 1% steps. Keeps accent-derived text readable on every surface.
 */
export function readableColor(target: string, background: string, minRatio = 4.5): string {
	if (contrastRatio(target, background) >= minRatio) return target;
	const dark = luminance(background) > 0.5 ? '#000000' : '#ffffff';
	for (let step = 1; step <= 100; step++) {
		const candidate = mixColor(target, dark, step / 100);
		if (contrastRatio(candidate, background) >= minRatio) return candidate;
	}
	return dark;
}

// ---------------------------------------------------------------------------
// Theme assembly.
// ---------------------------------------------------------------------------

export interface Appearance {
	mode: AppearanceMode;
	variables: Record<string, string>;
	config: ThemeConfig;
}

export function appearanceTheme(mode: AppearanceMode, accent = ACCENT): Appearance {
	const accentColor = /^#[\da-f]{6}$/i.test(accent) ? accent : ACCENT;
	const p = palettes[mode];
	const selected = mixColor(p.card, accentColor, mode === 'dark' ? 0.16 : 0.1);
	const primaryText = [selected, p.card, p.surface, p.muted, p.sidebar].reduce(
		(color, bg) => readableColor(color, bg),
		accentColor
	);
	const primary = readableColor(accentColor, '#ffffff');
	const onSidebar = (ink: string) => [p.sidebar, p.muted, selected].reduce(
		(color, background) => readableColor(color, background, 3),
		ink
	);
	const sidebarDetail = onSidebar(readableColor(
		mode === 'dark' ? accentColor : mixColor(accentColor, '#000000', 0.56),
		'#ffffff',
		mode === 'dark' ? 4 : 7.5
	));
	const sidebarBody = onSidebar(mixColor(sidebarDetail, '#ffffff', mode === 'dark' ? 0.6 : 0.4));

	const variables: Record<string, string> = {
		'--background': p.background,
		'--foreground': p.foreground,
		'--card': p.card,
		'--surface': p.surface,
		'--muted': p.muted,
		'--muted-foreground': p.secondary,
		'--border': p.border,
		'--control-border': p.controlBorder,
		'--sidebar-bg': p.sidebar,
		'--sidebar-icon-detail': sidebarDetail,
		'--sidebar-icon-body': sidebarBody,
		'--primary': primaryText,
		'--primary-foreground': mode === 'dark' ? palettes.dark.background : '#ffffff',
		'--accent': selected,
		'--status-success': p.good,
		'--status-warning': p.warning,
		'--status-danger': p.danger,
		'--status-info': p.info,
		'--status-neutral': p.secondary,
		'--font-sans': "'PingFang SC', 'Microsoft YaHei', sans-serif",
		'--font-mono': "Menlo, 'SFMono-Regular', Consolas, monospace",
		'--radius': '8px',
		'--radius-sm': '4px',
		'--radius-lg': '12px',
		'--space-1': '4px',
		'--space-2': '8px',
		'--space-3': '10px',
		'--space-4': '12px',
		'--space-5': '16px',
		'--text-caption': '12px',
		'--text-secondary': '13px',
		'--text-body': '14px',
		'--text-title': '16px',
		'--text-page': '20px',
		'--motion-fast': '100ms',
		'--motion-mid': '200ms',
		'--motion-slow': '300ms',
		'--topbar-height': '44px',
		'--topbar-control-height': '32px'
	};

	const config: ThemeConfig = {
		algorithm: mode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
		token: {
			colorPrimary: primary,
			colorPrimaryHover: readableColor(mixColor(primary, '#ffffff', 0.12), '#ffffff'),
			colorPrimaryActive: mixColor(primary, '#000000', 0.18),
			colorPrimaryText: primaryText,
			colorPrimaryTextHover: primaryText,
			colorPrimaryTextActive: primaryText,
			colorPrimaryBg: selected,
			colorPrimaryBgHover: selected,
			colorPrimaryBorder: readableColor(accentColor, p.card, 3),
			colorLink: primaryText,
			colorLinkHover: primaryText,
			colorLinkActive: primaryText,
			colorBgBase: p.background,
			colorBgLayout: p.background,
			colorBgContainer: p.card,
			colorBgElevated: p.elevated,
			colorText: p.foreground,
			colorTextHeading: p.foreground,
			colorTextSecondary: p.secondary,
			colorTextTertiary: p.secondary,
			colorTextDescription: p.secondary,
			colorTextPlaceholder: p.secondary,
			colorBgContainerDisabled: p.muted,
			colorBorder: p.controlBorder,
			colorBorderSecondary: p.border,
			colorSuccess: p.good,
			colorWarning: p.warning,
			colorError: p.danger,
			colorSuccessText: p.good,
			colorWarningText: p.warning,
			colorErrorText: p.danger,
			colorInfo: primary,
			colorInfoText: primaryText,
			borderRadius: 8,
			borderRadiusSM: 4,
			borderRadiusLG: 12,
			fontFamily: "'PingFang SC', 'Microsoft YaHei', sans-serif",
			fontSize: 14
		},
		components: {
			Layout: {
				bodyBg: p.background,
				headerBg: p.card,
				siderBg: p.sidebar,
				lightSiderBg: p.sidebar
			},
			Menu: {
				itemBg: p.sidebar,
				subMenuItemBg: p.sidebar,
				itemSelectedBg: selected,
				itemSelectedColor: primaryText,
				itemHoverBg: p.muted
			},
			Card: { headerBg: p.card },
			Segmented: {
				trackBg: p.muted,
				itemSelectedBg: selected,
				itemSelectedColor: primaryText
			},
			Table: { headerBg: p.surface, borderColor: p.border, rowHoverBg: p.surface },
			Button: { primaryColor: '#ffffff', primaryShadow: 'none' }
		}
	};

	return { mode, variables, config };
}
