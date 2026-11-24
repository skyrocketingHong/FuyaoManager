import type { HTMLAttributes } from 'react';

// robot-platform/libraries/ui/VersionText.tsx.
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
