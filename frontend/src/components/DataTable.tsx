import { Table, type TableProps } from 'antd';

// robot-platform's Widgets.DataTable uses small tables and a shared surface.
// Keep the manager's typed rows, stable keys, sorting, and server pagination.
export default function DataTable<Row extends object>({ className, ...props }: TableProps<Row>) {
	return (
		<Table<Row>
			size="small"
			{...props}
			className={['data-table', className].filter(Boolean).join(' ')}
		/>
	);
}
