/**
 * Aurora ribbon painter (robot-platform's modules/login/ui/src/
 * LoginAurora.tsx, commit b336dd75). Two ribbons of light drift across a
 * sky: each is a smooth curve through seven rows whose horizontal positions
 * were traced from the reference at 0.4-second intervals. Neighbouring
 * traced frames are interpolated, so the drift loops without a jump.
 */
export const auroraCycleMs = 7200;
const rowStart = -0.12;
const rowSpacing = 0.15;

// Horizontal centers of the seven rows, as fractions of the width, for each
// traced frame of the main ribbon.
const ribbonFrames = [
	[0.23, 0.4, 0.63, 0.85, 0.97, 1.04, 1.14],
	[0.15, 0.31, 0.56, 0.8, 0.94, 1.01, 1.17],
	[0.03, 0.23, 0.49, 0.74, 0.88, 0.97, 1.11],
	[0.03, 0.24, 0.48, 0.73, 0.84, 0.95, 1.08],
	[0.3, 0.47, 0.64, 0.68, 0.87, 0.95, 1.06],
	[0.48, 0.53, 0.57, 0.59, 0.79, 0.91, 1.06],
	[0.34, 0.46, 0.49, 0.52, 0.72, 0.9, 1.06],
	[0.1, 0.26, 0.36, 0.45, 0.66, 0.84, 1.01],
	[0.11, 0.22, 0.34, 0.49, 0.72, 0.92, 1.08],
	[0.14, 0.31, 0.44, 0.64, 0.88, 0.97, 1.13],
	[0.19, 0.34, 0.52, 0.76, 0.96, 1.03, 1.16],
	[0.17, 0.34, 0.55, 0.79, 0.93, 1.02, 1.16]
] as const;

// Glow strokes around each ribbon's core line: width relative to the short
// side of the canvas, and opacity.
const glowStrokes = [
	[0.31, 0.1],
	[0.22, 0.12],
	[0.15, 0.16],
	[0.1, 0.24],
	[0.06, 0.32]
] as const;

/** Catmull-Rom style position between two traced frames. */
function interpolate(before: number, start: number, end: number, after: number, fraction: number) {
	const startVelocity = (end - before) / 2;
	const endVelocity = (after - start) / 2;
	const distance = end - start;
	return (
		start +
		fraction *
			(startVelocity +
				fraction *
					(3 * distance - 2 * startVelocity - endVelocity +
						fraction * (startVelocity + endVelocity - 2 * distance)))
	);
}

function traceRibbon(context: CanvasRenderingContext2D, phase: number, offsetX: number, offsetY: number) {
	const count = ribbonFrames.length;
	const position = (phase % 1) * count;
	const index = Math.floor(position);
	const before = ribbonFrames[(index + count - 1) % count];
	const start = ribbonFrames[index];
	const end = ribbonFrames[(index + 1) % count];
	const after = ribbonFrames[(index + 2) % count];
	const { width, height } = context.canvas;
	const centers = start.map(
		(x, row) => (interpolate(before[row], x, end[row], after[row], position - index) + offsetX) * width
	);
	const step = rowSpacing * height;
	let y = (rowStart + offsetY) * height;

	context.beginPath();
	context.moveTo(centers[0], y);
	for (let row = 0; row < centers.length - 1; row++) {
		const startX = centers[row];
		const endX = centers[row + 1];
		const beforeX = centers[row - 1] ?? startX;
		const afterX = centers[row + 2] ?? endX;
		context.bezierCurveTo(
			startX + (endX - beforeX) / 6,
			y + step / 3,
			endX - (afterX - startX) / 6,
			y + (2 * step) / 3,
			endX,
			y + step
		);
		y += step;
	}
}

/** Returns a function that draws the aurora at a phase from 0 to 1. */
export function createAuroraPainter(context: CanvasRenderingContext2D) {
	const { width, height } = context.canvas;
	const scale = Math.min(width, height);
	const glow = context.createLinearGradient(0, 0, width * 0.25, height * 0.75);
	glow.addColorStop(0, '#178db6');
	glow.addColorStop(0.48, '#24b8b7');
	glow.addColorStop(1, '#65c4ad');
	const edge = context.createLinearGradient(0, 0, 0, height * 0.8);
	edge.addColorStop(0, '#299fc7');
	edge.addColorStop(0.42, '#54d0c0');
	edge.addColorStop(0.75, '#a2d89e');
	edge.addColorStop(1, '#79b6c7');
	context.lineCap = 'round';

	function strokeRibbon(phase: number, offsetX: number, offsetY: number, size: number, strength: number) {
		traceRibbon(context, phase, offsetX, offsetY);
		context.strokeStyle = glow;
		for (const [relativeWidth, opacity] of glowStrokes) {
			context.lineWidth = relativeWidth * scale * size;
			context.globalAlpha = opacity * strength;
			context.stroke();
		}
		context.strokeStyle = edge;
		context.lineWidth = 0.036 * scale * size;
		context.globalAlpha = 0.52 * strength;
		context.stroke();
	}

	// A fainter ribbon further out and up, then the main one.
	return (phase: number) => {
		context.clearRect(0, 0, width, height);
		strokeRibbon(phase + 0.16, -0.48, 0.12, 0.87, 0.61);
		strokeRibbon(phase, 0, 0, 1, 1);
	};
}
