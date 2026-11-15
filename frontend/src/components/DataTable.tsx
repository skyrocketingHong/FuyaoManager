import type { HTMLAttributes, Key, ReactNode } from 'react';
import { Table, type TableProps } from 'antd';

interface MatrixTableProps {
	heads: string[];
	rows: ReactNode[][];
	empty?: ReactNode;
	rowProps?: (index: number) => HTMLAttributes<HTMLTableRowElement>;
	columnWidths?: (number | string | undefined)[];
	className?: string;
	minWidth?: number;
	rowKeys?: Key[];
	pagination?: TableProps<ReactNode[]>['pagination'];
	styles?: TableProps<ReactNode[]>['styles'];
}

// robot-platform's Widgets.DataTable uses small tables and a shared surface.
// Keep the manager's typed rows, stable keys, sorting, and server pagination.
export default function DataTable<Row extends object>(props: TableProps<Row> | MatrixTableProps) {
	if ('heads' in props) {
		const {
			heads,
			rows,
			empty,
			rowProps,
			columnWidths,
			className = '',
			minWidth,
			rowKeys,
			styles,
			pagination = false
		} = props;
		const keys = new Map(rows.map((row, index) => [row, rowKeys?.[index] ?? String(index)]));
		return (
			<Table<ReactNode[]>
				className={`data-table ${className}`.trim()}
				size="small"
				pagination={pagination}
				styles={styles}
				locale={{ emptyText: empty }}
				columns={heads.map((head, column) => ({
					title: head,
					width: columnWidths?.[column],
					render: (_, record) => record[column]
				}))}
				dataSource={rows}
				rowKey={(record) => keys.get(record)!}
				onRow={(_, index) => (rowProps ? rowProps(index ?? 0) : {})}
				scroll={minWidth ? { x: minWidth } : undefined}
			/>
		);
	}
	const { className, ...tableProps } = props;
	return (
		<Table<Row>
			size="small"
			{...tableProps}
			className={['data-table', className].filter(Boolean).join(' ')}
		/>
	);
}
