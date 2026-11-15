import { DatePicker, Input, TimePicker } from 'antd';
import dayjs from 'dayjs';
import { t } from '../lib/i18n';

type DateParts = { date: string; time: string; fraction: string; zone: string };

function splitDate(value: string): DateParts | null {
	const match =
		/^(\d{4}-\d{2}-\d{2})(?:T(\d{2}:\d{2})(?::(\d{2})(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?)?$/.exec(value);
	if (!match) return null;
	const [year, month, day] = match[1].split('-').map(Number);
	const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
	const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
	if (month < 1 || month > 12 || day < 1 || day > days[month - 1]) return null;
	if (match[2]) {
		const [hours, minutes] = match[2].split(':').map(Number);
		if (hours > 23 || minutes > 59 || Number(match[3] ?? 0) > 59) return null;
	}
	if (match[5] && match[5] !== 'Z') {
		if (Number(match[5].slice(1, 3)) > 23 || Number(match[5].slice(4, 6)) > 59) return null;
	}
	return {
		date: match[1],
		time: match[2] ? `${match[2]}${match[3] ? `:${match[3]}` : ''}` : '',
		fraction: match[4] ?? '',
		zone: match[5] ?? ''
	};
}

function pickerDate(value: string) {
	const [year, month, day] = value.split('-').map(Number);
	const date = new Date(0);
	date.setFullYear(year, month - 1, day);
	date.setHours(12, 0, 0, 0);
	return dayjs(date);
}

export function isValidPostDate(value: string): boolean {
	return splitDate(value) !== null;
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
	const parts = value ? splitDate(value) : null;
	if (value && !parts) {
		return (
			<Input
				id={id}
				value={value}
				onChange={(event) => onChange?.(event.target.value)}
				disabled={disabled}
			/>
		);
	}
	const clock = parts?.time
		? dayjs(`2000-01-01T${parts.time.length === 5 ? `${parts.time}:00` : parts.time}`)
		: null;
	return (
		<div className="post-date-input">
			<DatePicker
				id={id}
				value={parts ? pickerDate(parts.date) : null}
				disabled={disabled}
				onChange={(next) => {
					if (!next) return onChange?.('');
					const tail = parts?.time ? `T${parts.time}${parts.fraction}${parts.zone}` : '';
					onChange?.(`${next.format('YYYY-MM-DD')}${tail}`);
				}}
			/>
			<TimePicker
				aria-label={`${label} ${t('posts.time')}`}
				value={clock}
				format="HH:mm:ss"
				disabled={disabled || !parts}
				onChange={(next) => {
					if (!parts) return;
					if (!next) return onChange?.(parts.date);
					onChange?.(`${parts.date}T${next.format('HH:mm:ss')}${parts.fraction}${parts.zone}`);
				}}
			/>
			{parts?.zone ? (
				<span className="post-date-zone" title={t('posts.timezone')}>
					{parts.zone}
				</span>
			) : null}
		</div>
	);
}
