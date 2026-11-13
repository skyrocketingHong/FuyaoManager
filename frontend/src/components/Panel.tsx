/**
 * Panel and TablePanel: the two content surfaces of the console. Panel is
 * the standard card (title + optional tags + body); TablePanel is for
 * standalone lists — the table itself is the only content surface, no extra
 * card wrapper. The table section carries an aria-labelledby binding for
 * its header (ported from the robot platform's shared/cards/Panel.tsx,
 * read-only reference) — no workspace grid rounding, which belongs to that
 * platform's 16:9 stage.
 */
import { useId } from 'react';
import type { ReactNode } from 'react';
import { Card } from 'antd';

export function Panel({
	title,
	tags,
	flush,
	className,
	children
}: {
	title?: ReactNode;
	tags?: ReactNode;
	/** Flush bodies skip the default padding (tables, code blocks). */
	flush?: boolean;
	className?: string;
	children: ReactNode;
}) {
	return (
		<Card
			className={['panel', flush ? 'panel--flush' : '', className ?? ''].filter(Boolean).join(' ')}
			title={title}
			extra={tags ? <span className="panel-tags">{tags}</span> : undefined}
			variant="outlined"
		>
			{flush ? children : <div className="panel-body">{children}</div>}
		</Card>
	);
}

export function TablePanel({
	title,
	tools,
	tags,
	className,
	children
}: {
	title?: ReactNode;
	tools?: ReactNode;
	tags?: ReactNode;
	className?: string;
	children: ReactNode;
}) {
	const titleId = useId();
	return (
		<section
			className={['panel', 'panel--table', 'table-panel', className].filter(Boolean).join(' ')}
			data-card-template="table"
			aria-labelledby={title ? titleId : undefined}
		>
			{title || tools || tags ? (
				<header className="table-panel-header">
					{title ? <h2 id={title ? titleId : undefined}>{title}</h2> : null}
					{tools ? <div className="table-panel-tools">{tools}</div> : null}
					{tags ? <span className="panel-tags">{tags}</span> : null}
				</header>
			) : null}
			<div className="table-panel-content">{children}</div>
		</section>
	);
}
