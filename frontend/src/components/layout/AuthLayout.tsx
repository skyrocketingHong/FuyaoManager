import type { ReactNode } from 'react';
import { t } from '../../lib/i18n';

export default function AuthLayout({ children }: { children: ReactNode }) {
	return (
		<main className="auth-screen auth-layout">
			<div className="auth-layout-inner">
				<header className="auth-identity">
					<h1>FuyaoManager</h1>
					<p>{t('auth.workspace')}</p>
				</header>
				<div className="auth-stack">{children}</div>
			</div>
		</main>
	);
}
