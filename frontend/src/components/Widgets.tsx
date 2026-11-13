import type { ReactNode } from 'react';
import StatusTag, { type StatusKind } from './StatusTag';
export { Panel, TablePanel } from './Panel';
export { default as DataTable } from './DataTable';
export { default as VersionText } from './VersionText';
export { timeText } from '../lib/admin/presentation';

export function Tag({
	tone,
	children,
	className
}: {
	tone: StatusKind;
	children: ReactNode;
	className?: string;
}) {
	return (
		<StatusTag kind={tone} className={className}>
			{children}
		</StatusTag>
	);
}
