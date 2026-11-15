// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import DataTable from '../src/components/DataTable';

afterEach(cleanup);

it('cycles ascending, descending and the original order for local rows', () => {
	render(<DataTable rowKey="id" pagination={false} dataSource={[
		{ id: 'b', name: 'Beta' }, { id: 'a', name: 'Alpha' }
	]} columns={[{ title: 'Name', dataIndex: 'name' }]} />);
	const header = screen.getByRole('columnheader', { name: /Name/ });
	const names = () => within(screen.getAllByRole('rowgroup')[1]).getAllByRole('row').map((row) => row.textContent);
	expect(names()).toEqual(['Beta', 'Alpha']);
	fireEvent.click(header);
	expect(header.getAttribute('aria-sort')).toBe('ascending');
	expect(names()).toEqual(['Alpha', 'Beta']);
	fireEvent.click(header);
	expect(header.getAttribute('aria-sort')).toBe('descending');
	expect(names()).toEqual(['Beta', 'Alpha']);
	fireEvent.click(header);
	expect(header.getAttribute('aria-sort')).toBeNull();
	expect(names()).toEqual(['Beta', 'Alpha']);
});
