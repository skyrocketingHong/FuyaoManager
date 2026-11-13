import { Button, Tooltip, type ButtonProps } from 'antd';
import { useId } from 'react';

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
