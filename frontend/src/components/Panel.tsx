/**
 * Panel and TablePanel: the two content surfaces of the console. Panel is
 * the standard card (title + optional tags + body); TablePanel is for
 * standalone lists — the table itself is the only content surface, no extra
 * card wrapper.
 */
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
			extra={tags}
			variant="outlined"
		>
			{flush ? children : <div className="panel-body">{children}</div>}
		</Card>
	);
}

export function TablePanel({
	title,
	tools,
	children
}: {
	title?: ReactNode;
	tools?: ReactNode;
	children: ReactNode;
}) {
	return (
		<section className="table-panel">
			{title || tools ? (
				<header className="table-panel-header">
					<h2>{title}</h2>
					{tools ? <div className="table-panel-tools">{tools}</div> : null}
				</header>
			) : null}
			<div className="table-panel-content">{children}</div>
		</section>
	);
}
