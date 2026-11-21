import { Button, Popover, type ButtonProps } from 'antd';
import type { ReactNode } from 'react';
import { t } from '../lib/i18n';
import VersionText from './VersionText';

export default function VersionBadge({
	version,
	label,
	details,
	onOpenChange,
	size = 'small'
}: {
	version: string;
	label?: string;
	details: ReactNode;
	onOpenChange?: (open: boolean) => void;
	size?: ButtonProps['size'];
}) {
	return (
		<Popover
			trigger="click"
			title={t('common.buildDetails')}
			content={details}
			onOpenChange={onOpenChange}
		>
			<Button
				type="text"
				size={size}
				className="version-badge"
				aria-label={t('common.versionDetails', { version })}
			>
				{label && <span className="version-badge-label">{label}</span>}
				<VersionText>{version}</VersionText>
			</Button>
		</Popover>
	);
}
