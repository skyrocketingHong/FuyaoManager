import React from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import App from './App';
import AppearanceProvider from './components/AppearanceProvider';
import './styles/base.css';
import './styles/shell.css';
import './styles/sidebar/sidebar.css';
import './styles/pages.css';

// A data router is required for the unsaved-changes navigation blocker.
const router = createBrowserRouter([{ path: '*', element: <App /> }]);

ReactDOM.createRoot(document.getElementById('root')!).render(
	<React.StrictMode>
		<AppearanceProvider>
			<RouterProvider router={router} />
		</AppearanceProvider>
	</React.StrictMode>
);
