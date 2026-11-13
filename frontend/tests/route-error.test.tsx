// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import RouteErrorPage from '../src/components/RouteErrorPage';
import { setLocale } from '../src/lib/i18n';

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it('offers recovery instead of rendering an exception stack', async () => {
	setLocale('en-US');
	vi.spyOn(console, 'error').mockImplementation(() => {});
	function BrokenPage(): never { throw new Error('private exception detail'); }
	const router = createMemoryRouter([{
		path: '*', element: <BrokenPage />, errorElement: <RouteErrorPage />
	}]);
	render(<ConfigProvider><RouterProvider router={router} /></ConfigProvider>);
	expect(await screen.findByText('This page could not be displayed')).toBeDefined();
	expect(screen.getByRole('button', { name: 'Reload page' })).toBeDefined();
	expect(screen.getByRole('link', { name: 'Platform home' }).getAttribute('href')).toBe('/sites');
	expect(screen.queryByText(/private exception detail|Unexpected Application Error/)).toBeNull();
	expect(document.title).toBe('Fuyao Manager - This page could not be displayed');
});
