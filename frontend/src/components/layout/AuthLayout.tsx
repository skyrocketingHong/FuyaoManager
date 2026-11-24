/**
 * AuthLayout: the shared surface of the sign-in and passkey pages — the
 * product identity and the form side by side, over the aurora of the robot
 * platform's login page (modules/login/ui/src/LoginSurface.tsx and commit
 * b336dd75). The aurora is decorative and takes no pointer; a control in the
 * bottom corner pauses it. When the system asks to reduce motion the aurora
 * stays still from the start and the control is left out.
 */
import { useState, type ReactNode } from 'react';
import AuthAurora from '../auth/AuthAurora';
import { t } from '../../lib/i18n';
import { useReducedMotion } from '../../lib/ui/useReducedMotion';

export default function AuthLayout({ children }: { children: ReactNode }) {
	const reduced = useReducedMotion();
	const [paused, setPaused] = useState(false);
	return (
		<main className="auth-screen auth-layout" data-motion-paused={paused && !reduced ? '' : undefined}>
			<div className="auth-atmosphere" aria-hidden="true">
				<AuthAurora moving={!reduced && !paused} />
			</div>
			<div className="auth-layout-inner">
				<header className="auth-identity">
					<h1>FuyaoManager</h1>
					<p>{t('auth.workspace')}</p>
				</header>
				<div className="auth-stack">{children}</div>
			</div>
			{!reduced && (
				<button type="button" className="auth-motion-toggle" onClick={() => setPaused((value) => !value)}>
					{paused ? t('auth.resumeMotion') : t('auth.pauseMotion')}
				</button>
			)}
		</main>
	);
}
