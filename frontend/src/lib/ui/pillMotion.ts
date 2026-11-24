// Ported from robot-platform/libraries/ui/operationbar/pillMotion.ts. The
// robot platform's rules set this motion for capsule surfaces outside the
// fast, mid and slow tokens: 420ms with a strong ease-out, so the surface
// grows quickly and settles without overshoot.
const resizeMotion = { duration: 420, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' };

export type PillSize = Pick<DOMRect, 'width' | 'height'>;

/**
 * Animates `node` from one size to another with the Web Animations API, so
 * an interruption continues from the size shown at that moment. Returns
 * nothing when reduced motion is set, a size is not measurable yet, the sizes
 * already match or the browser cannot animate; the surface then shows its
 * final size directly.
 */
export function animatePillSize(
	node: Pick<HTMLElement, 'animate'>,
	from: PillSize | undefined,
	to: PillSize | undefined,
	reducedMotion: boolean,
	closing = false
): Animation | undefined {
	if (
		reducedMotion ||
		!from?.width ||
		!from.height ||
		!to?.width ||
		!to.height ||
		(from.width === to.width && from.height === to.height)
	)
		return;
	try {
		return node.animate(
			[from, to].map(({ width, height }) => ({
				width: `${width}px`,
				height: `${height}px`,
				overflow: 'clip'
			})),
			{ ...resizeMotion, fill: closing ? 'both' : 'backwards' }
		);
	} catch {
		return;
	}
}
