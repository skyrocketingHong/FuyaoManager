import type { HTMLAttributes } from 'react';

// robot-platform/components/platform/shared/VersionText.tsx.
export default function VersionText({
	className = '',
	children,
	...props
}: HTMLAttributes<HTMLElement>) {
	return (
		<span {...props} className={`version-text ${className}`.trim()}>
			{children}
		</span>
	);
}
