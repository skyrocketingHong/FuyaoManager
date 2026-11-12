/**
 * StatusTag: five semantic statuses with colors independent of the accent.
 */
import type { ReactNode } from 'react';

export type StatusKind = 'success' | 'warning' | 'danger' | 'info' | 'neutral';

export default function StatusTag({ kind, children }: { kind: StatusKind; children: ReactNode }) {
	return <span className={`status-tag status-tag--${kind}`}>{children}</span>;
}
