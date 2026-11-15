import type { SorterResult, SortOrder } from 'antd/es/table/interface';

export type TableSort = { field: string; direction: 'asc' | 'desc' };

export function sortColumn(field: string, current: TableSort | undefined) {
	const sortOrder: SortOrder = current?.field === field
		? (current.direction === 'asc' ? 'ascend' : 'descend')
		: null;
	return { key: field, sorter: true as const, sortOrder };
}

export function selectedTableSort<Row>(sorter: SorterResult<Row> | SorterResult<Row>[]): TableSort | undefined {
	const selected = Array.isArray(sorter) ? sorter[0] : sorter;
	if (!selected?.columnKey || !selected.order) return undefined;
	return { field: String(selected.columnKey), direction: selected.order === 'ascend' ? 'asc' : 'desc' };
}
