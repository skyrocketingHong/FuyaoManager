// Adapted from robot-platform's settings/AppearancePage.tsx.
import { useSyncExternalStore } from 'react';
import { Button, ColorPicker, Radio } from 'antd';
import { Check, Monitor, Moon, Sun } from 'lucide-react';
import { Panel } from '../components/Panel';
import StatusTag from '../components/StatusTag';
import DateTimeSettings from '../features/settings/DateTimeSettings';
import { usePageHeader } from '../components/PageHeader';
import { t, type MessageKey } from '../lib/i18n';
import { resetTimePreferences } from '../lib/time';
import {
	accentPresets,
	resetAppearance,
	setAppearance,
	subscribeTheme,
	themeSnapshot,
	type ThemeMode
} from '../lib/theme';

export default function AppearancePage() {
	const preferences = useSyncExternalStore(subscribeTheme, themeSnapshot);
	const selected = accentPresets.find((preset) => preset.color === preferences.accent);
	const content = (
		<div className="topbar-appearance-content" aria-live="polite">
			<span>{t(preferences.dark ? 'appearance.dark' : 'appearance.light')}</span>
			<StatusTag kind="info">
				{selected ? t(`appearance.color.${selected.name}` as MessageKey) : t('appearance.custom')}
			</StatusTag>
		</div>
	);
	const actions = (
		<Button
			onClick={() => {
				resetAppearance();
				resetTimePreferences();
			}}
		>
			{t('appearance.reset')}
		</Button>
	);
	const inHeader = usePageHeader({ content, actions });
	return (
		<div className="page-stack appearance-page">
			{!inHeader && (
				<div className="table-panel-header">
					{content}
					{actions}
				</div>
			)}
			<Panel>
				<div className="appearance-options">
					<section className="appearance-section" aria-labelledby="appearance-mode-title">
						<h2 id="appearance-mode-title">{t('appearance.mode')}</h2>
						<Radio.Group
							className="appearance-modes"
							name="appearance-mode"
							value={preferences.mode}
							onChange={(event) => setAppearance({ mode: event.target.value as ThemeMode })}
							aria-labelledby="appearance-mode-title"
						>
							<Radio.Button value="light">
								<Sun size={16} />
								{t('appearance.light')}
							</Radio.Button>
							<Radio.Button value="dark">
								<Moon size={16} />
								{t('appearance.dark')}
							</Radio.Button>
							<Radio.Button value="system">
								<Monitor size={16} />
								{t('appearance.system')}
							</Radio.Button>
						</Radio.Group>
					</section>
					<section className="appearance-section" aria-labelledby="appearance-accent-title">
						<h2 id="appearance-accent-title">{t('appearance.accent')}</h2>
						<div
							className="appearance-palette"
							role="group"
							aria-labelledby="appearance-accent-title"
						>
							{accentPresets.map((preset) => (
								<Button
									key={preset.color}
									className={`appearance-color${selected?.color === preset.color ? ' is-selected' : ''}`}
									aria-pressed={selected?.color === preset.color}
									onClick={() => setAppearance({ accent: preset.color })}
								>
									<span
										className="appearance-swatch"
										style={{ backgroundColor: preset.color }}
										aria-hidden="true"
									/>
									<span>{t(`appearance.color.${preset.name}` as MessageKey)}</span>
									{selected?.color === preset.color && <Check size={16} aria-hidden="true" />}
								</Button>
							))}
						</div>
						<div className="appearance-custom">
							<span id="appearance-custom-label">{t('appearance.custom')}</span>
							<ColorPicker
								value={preferences.accent}
								format="hex"
								disabledAlpha
								onChange={(color) => setAppearance({ accent: color.toHexString() })}
							>
								<Button aria-labelledby="appearance-custom-label">
									<span
										className="appearance-swatch"
										style={{ backgroundColor: preferences.accent }}
										aria-hidden="true"
									/>
									<code>{preferences.accent.toUpperCase()}</code>
								</Button>
							</ColorPicker>
						</div>
					</section>
					<DateTimeSettings />
				</div>
			</Panel>
		</div>
	);
}
