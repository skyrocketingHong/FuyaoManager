import { Button, Tooltip, type ButtonProps } from 'antd';
import { useId } from 'react';
import { RefreshCw } from 'lucide-react';
import { t } from '../lib/i18n';

// robot-platform/components/platform/shared/ActionButton.tsx.
export default function ActionButton({
	disabledReason,
	disabled,
	loading,
	...props
}: ButtonProps & { disabledReason?: string }) {
	const reasonId = useId();
	const button = (
		<Button
			{...props}
			disabled={!!disabledReason || !!disabled}
			loading={loading}
			aria-describedby={disabledReason ? reasonId : props['aria-describedby']}
		/>
	);
	return disabledReason ? (
		<Tooltip title={disabledReason} trigger={['hover', 'focus']}>
			<span className="action-button-hint" tabIndex={0} aria-describedby={reasonId}>
				{button}
				<span className="sr-only" id={reasonId}>
					{disabledReason}
				</span>
			</span>
		</Tooltip>
	) : (
		button
	);
}

type StandardActionProps = Omit<ButtonProps, 'children' | 'icon'> & { disabledReason?: string };

export function RefreshButton(props: StandardActionProps) {
	return (
		<ActionButton
			{...props}
			aria-label={props['aria-label'] ?? t('common.refresh')}
			icon={<RefreshCw size={16} aria-hidden="true" />}
		>
			{t('common.refresh')}
		</ActionButton>
	);
}

export function RetryButton(props: StandardActionProps) {
	return (
		<ActionButton
			{...props}
			aria-label={props['aria-label'] ?? t('common.retry')}
			icon={<RefreshCw size={16} aria-hidden="true" />}
		>
			{t('common.retry')}
		</ActionButton>
	);
}
