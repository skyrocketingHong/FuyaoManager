/**
 * StatusTag: five semantic statuses with colors independent of the accent.
 */
import type { ReactNode } from 'react';

export type StatusKind = 'success' | 'warning' | 'danger' | 'info' | 'neutral';

export default function StatusTag({
	kind,
	children,
	className = ''
}: {
	kind: StatusKind;
	children: ReactNode;
	className?: string;
}) {
	return <span className={`status-tag status-tag--${kind} ${className}`.trim()}>{children}</span>;
}
