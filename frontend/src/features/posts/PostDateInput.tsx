import { useEffect, useState, useSyncExternalStore } from 'react';
import { DatePicker, Input, TimePicker } from 'antd';
import dayjs from 'dayjs';
import { t } from '../../lib/i18n';
import {
	dateTimeValue,
	displayTimezone,
	subscribeTime,
	timeSnapshot,
	postDateParts,
	wallTimeToUTC
} from '../../lib/time';

function pickerDate(value: string) {
	const [year, month, day] = value.split('-').map(Number);
	const date = new Date(0);
	date.setFullYear(year, month - 1, day);
	date.setHours(12, 0, 0, 0);
	return dayjs(date);
}

export function isValidPostDate(value: string): boolean {
	return postDateParts(value) !== null;
}

export default function PostDateInput({
	id,
	value = '',
	onChange,
	disabled,
	label
}: {
	id?: string;
	value?: string;
	onChange?: (value: string) => void;
	disabled?: boolean;
	label: string;
}) {
	const preferences = useSyncExternalStore(subscribeTime, timeSnapshot);
	const [invalidWall, setInvalidWall] = useState(false);
	useEffect(() => {
		setInvalidWall(false);
	}, [value, preferences.timezone]);
	const parts = value ? postDateParts(value) : null;
	const displayed = parts?.time ? dateTimeValue(value, preferences.timezone) : null;
	if (value && (!parts || (parts.time && !displayed))) {
		return (
			<Input
				id={id}
				value={value}
				onChange={(event) => onChange?.(event.target.value)}
				disabled={disabled}
			/>
		);
	}
	const date = displayed?.format('YYYY-MM-DD') ?? parts?.date ?? '';
	const time = displayed ? displayed.format('HH:mm:ss') : '';
	const clock = time ? dayjs(`2000-01-01T${time}`) : null;
	const offset = displayed?.format('Z');
	const zoneLabel = offset === '+00:00' ? 'UTC' : offset ? `UTC${offset}` : '';
	function update(wallDate: string, wallTime: string, fraction: string) {
		const result = wallTime
			? wallTimeToUTC(wallDate, wallTime, fraction, preferences.timezone)
			: wallDate;
		if (result === null) {
			setInvalidWall(true);
			return;
		}
		setInvalidWall(false);
		onChange?.(result);
	}
	return (
		<div className="post-date-input">
			<DatePicker
				id={id}
				value={date ? pickerDate(date) : null}
				disabled={disabled}
				onChange={(next) => {
					if (!next) {
						setInvalidWall(false);
						onChange?.('');
						return;
					}
					update(next.format('YYYY-MM-DD'), time, parts?.fraction ?? '');
				}}
			/>
			<TimePicker
				aria-label={`${label} ${t('posts.time')}`}
				value={clock}
				format="HH:mm:ss"
				disabled={disabled || !parts}
				onChange={(next) => {
					if (!parts) return;
					update(date, next?.format('HH:mm:ss') ?? '', next ? parts.fraction : '');
				}}
			/>
			{zoneLabel ? (
				<span className="post-date-zone" title={displayTimezone(preferences.timezone)}>
					{zoneLabel}
				</span>
			) : null}
			{invalidWall ? <span role="alert">{t('datetime.invalidLocalTime')}</span> : null}
		</div>
	);
}
