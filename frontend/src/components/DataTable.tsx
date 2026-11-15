import { Table, type TableProps } from 'antd';

type SortableTableProps<Row extends object> = TableProps<Row> & { manualSorting?: boolean };
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

export function compareTableValues(left: unknown, right: unknown): number {
	if (left === null || left === undefined) return right === null || right === undefined ? 0 : 1;
	if (right === null || right === undefined) return -1;
	if (typeof left === 'number' && typeof right === 'number') return left - right;
	if (typeof left === 'boolean' && typeof right === 'boolean') return Number(left) - Number(right);
	const a = Array.isArray(left) ? left.join(', ') : String(left);
	const b = Array.isArray(right) ? right.join(', ') : String(right);
	if (/^\d{4}-\d{2}-\d{2}T/.test(a) && /^\d{4}-\d{2}-\d{2}T/.test(b)) {
		const first = Date.parse(a); const second = Date.parse(b);
		if (!Number.isNaN(first) && !Number.isNaN(second)) return first - second;
	}
	return collator.compare(a, b);
}

function indexedValue(row: object, dataIndex: string | number | readonly (string | number)[]): unknown {
	const path = Array.isArray(dataIndex) ? dataIndex : [dataIndex];
	return path.reduce<unknown>((value, key) => value && typeof value === 'object' ? (value as Record<string | number, unknown>)[key] : undefined, row);
}

function sortableColumns<Row extends object>(columns: NonNullable<TableProps<Row>['columns']>): NonNullable<TableProps<Row>['columns']> {
	return columns.map((column) => {
		if ('children' in column && column.children) return { ...column, children: sortableColumns(column.children) };
		if (!('dataIndex' in column) || column.dataIndex === undefined || column.sorter !== undefined) return column;
		const index = column.dataIndex as string | number | readonly (string | number)[];
		return { ...column, sorter: (left: Row, right: Row) => compareTableValues(indexedValue(left, index), indexedValue(right, index)) };
	});
}

// robot-platform's Widgets.DataTable uses small tables and a shared surface.
// Keep the manager's typed rows, stable keys, sorting, and server pagination.
export default function DataTable<Row extends object>(props: SortableTableProps<Row>) {
	const { className, manualSorting = false, columns, ...tableProps } = props;
	return (
		<Table<Row>
			size="small"
			{...tableProps}
			sortDirections={['ascend', 'descend']}
			columns={columns && !manualSorting ? sortableColumns(columns) : columns}
			className={['data-table', className].filter(Boolean).join(' ')}
		/>
	);
}
