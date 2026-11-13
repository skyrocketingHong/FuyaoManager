import { siCaddy } from 'simple-icons';

export default function CaddyIcon({ size = 16 }: { size?: number }) {
	return (
		<svg
			width={size}
			height={size}
			viewBox="0 0 24 24"
			fill="currentColor"
			aria-hidden="true"
			focusable="false"
		>
			<path d={siCaddy.path} />
		</svg>
	);
}
