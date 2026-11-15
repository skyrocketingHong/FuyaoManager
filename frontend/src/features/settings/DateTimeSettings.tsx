import { useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { Button, Form, Input, Select, Space, Tooltip } from 'antd';
import { Info } from 'lucide-react';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';
import {
	displayTimezone,
	setTimePreferences,
	subscribeTime,
	timeSnapshot,
	timezones
} from '../../lib/time';

export default function DateTimeSettings() {
	const preferences = useSyncExternalStore(subscribeTime, timeSnapshot);
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const [dateFormat, setDateFormat] = useState(preferences.dateFormat);
	const [dateTimeFormat, setDateTimeFormat] = useState(preferences.dateTimeFormat);
	const zones = useMemo(
		() => [
			{ value: 'system', label: `${t('datetime.system')} (${displayTimezone('system')})` },
			...timezones().map((value) => ({ value, label: value }))
		],
		[locale]
	);
	useEffect(() => {
		setDateFormat(preferences.dateFormat);
		setDateTimeFormat(preferences.dateTimeFormat);
	}, [preferences.dateFormat, preferences.dateTimeFormat]);
	function commit() {
		setTimePreferences({ dateFormat, dateTimeFormat });
	}
	return (
		<section className="appearance-section datetime-settings" aria-labelledby="datetime-title">
			<h2 id="datetime-title">{t('datetime.title')}</h2>
			<Form layout="vertical">
				<Form.Item label={t('datetime.timezone')}>
					<Select
						aria-label={t('datetime.timezone')}
						value={preferences.timezone}
						showSearch={{ optionFilterProp: 'label' }}
						options={zones}
						onChange={(timezone) => setTimePreferences({ timezone })}
					/>
				</Form.Item>
				<Form.Item label={t('datetime.preset')}>
					<Select
						aria-label={t('datetime.preset')}
						value={preferences.preset}
						options={[
							{ value: 'iso', label: 'ISO 8601' },
							{ value: 'readable', label: t('datetime.readable') },
							{ value: 'custom', label: t('datetime.custom') }
						]}
						onChange={(preset: 'iso' | 'readable' | 'custom') => {
							if (preset === 'iso')
								setTimePreferences({
									preset,
									dateFormat: 'YYYY-MM-DD',
									dateTimeFormat: 'YYYY-MM-DDTHH:mm:ssZ'
								});
							else if (preset === 'readable')
								setTimePreferences({
									preset,
									dateFormat: 'YYYY-MM-DD',
									dateTimeFormat: 'YYYY-MM-DD HH:mm:ss Z'
								});
							else setTimePreferences({ preset });
						}}
					/>
				</Form.Item>
				{preferences.preset === 'custom' ? (
					<>
						<Form.Item
							label={
								<Space size={4}>
									{t('datetime.dateFormat')}
									<Tooltip title={t('datetime.formatHint')}>
										<Button
											type="text"
											size="small"
											icon={<Info size={16} aria-hidden="true" />}
											aria-label={t('datetime.formatHint')}
										/>
									</Tooltip>
								</Space>
							}
						>
							<Input
								aria-label={t('datetime.dateFormat')}
								value={dateFormat}
								maxLength={100}
								onChange={(event) => setDateFormat(event.target.value)}
								onBlur={commit}
								onPressEnter={(event) => {
									if (!event.nativeEvent.isComposing) commit();
								}}
							/>
						</Form.Item>
						<Form.Item label={t('datetime.dateTimeFormat')}>
							<Input
								aria-label={t('datetime.dateTimeFormat')}
								value={dateTimeFormat}
								maxLength={100}
								onChange={(event) => setDateTimeFormat(event.target.value)}
								onBlur={commit}
								onPressEnter={(event) => {
									if (!event.nativeEvent.isComposing) commit();
								}}
							/>
						</Form.Item>
					</>
				) : null}
			</Form>
		</section>
	);
}
