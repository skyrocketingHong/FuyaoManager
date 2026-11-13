import React from 'react';
import ReactDOM from 'react-dom/client';
import { StyleProvider } from '@ant-design/cssinjs';
import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import App from './App';
import AppearanceProvider from './components/AppearanceProvider';
import RouteErrorPage from './components/RouteErrorPage';
import './styles/base.css';
import './styles/shell.css';
import './styles/sidebar/sidebar.css';
import './styles/statusbar/statusbar.css';
import './styles/pages.css';
import './styles/appearance.css';

// A data router is required for the unsaved-changes navigation blocker.
const router = createBrowserRouter([
	{ path: '*', element: <App />, errorElement: <RouteErrorPage /> }
]);

ReactDOM.createRoot(document.getElementById('root')!).render(
	<React.StrictMode>
		<StyleProvider layer>
			<AppearanceProvider>
				<RouterProvider router={router} />
			</AppearanceProvider>
		</StyleProvider>
	</React.StrictMode>
);
