// Timing reference: robot-platform/libraries/ui/operationbar/pillMotion.ts
// (f5f1b9ae). The exit uses its own shorter duration and easing.
const openMotion = { duration: 420, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' };
const closeMotion = { duration: 300, easing: 'ease-in-out' };

export type PillBounds = Pick<DOMRect, 'left' | 'top' | 'width' | 'height'>;

export function samePillBounds(left: PillBounds, right: PillBounds): boolean {
	return left.left === right.left && left.top === right.top &&
		left.width === right.width && left.height === right.height;
}

/** Recover the displayed frame before the same transform was applied to the new layout. */
export function rebasePillBounds(
	displayed: PillBounds,
	previous: PillBounds,
	layout: PillBounds
): PillBounds {
	return {
		left: displayed.left + previous.left - layout.left,
		top: displayed.top + previous.top - layout.top,
		width: displayed.width * previous.width / layout.width,
		height: displayed.height * previous.height / layout.height
	};
}

function frame(rect: PillBounds, layout: PillBounds): Keyframe {
	return {
		transform: `translate(${rect.left - layout.left}px, ${rect.top - layout.top}px) scale(${rect.width / layout.width}, ${rect.height / layout.height})`,
		transformOrigin: 'top left',
		overflow: 'clip'
	};
}

/**
 * Maps the displayed bounds to the surface's final layout box. Including
 * both axes keeps the origin attached when the popover flips or right-aligns.
 */
export function animatePillBounds(
	node: Pick<HTMLElement, 'animate'>,
	from: PillBounds | undefined,
	to: PillBounds | undefined,
	layout: PillBounds,
	reducedMotion: boolean,
	closing = false
): Animation | undefined {
	if (
		reducedMotion ||
		!from?.width ||
		!from.height ||
		!to?.width ||
		!to.height ||
		!layout.width ||
		!layout.height ||
		samePillBounds(from, to)
	)
		return;
	try {
		return node.animate([frame(from, layout), frame(to, layout)], {
			...(closing ? closeMotion : openMotion),
			fill: closing ? 'both' : 'backwards'
		});
	} catch {
		return;
	}
}
