/**
 * Aurora painter tests (the robot platform's login aurora, ported for the
 * sign-in pages): a recording 2D context shows what the painter draws. The
 * painter is plain code, so this runs without a DOM.
 */
import { describe, expect, it, vi } from 'vitest';
import { auroraCycleMs, createAuroraPainter } from '../src/lib/ui/aurora';

type Stop = [number, string];

function recordingContext(width: number, height: number) {
	const gradients: Stop[][] = [];
	const strokes: { alpha: number; width: number }[] = [];
	const context = {
		canvas: { width, height },
		lineCap: '',
		strokeStyle: undefined as unknown,
		lineWidth: 0,
		globalAlpha: 1,
		clearRect: vi.fn(),
		beginPath: vi.fn(),
		moveTo: vi.fn(),
		bezierCurveTo: vi.fn(),
		stroke: vi.fn(),
		createLinearGradient: vi.fn(() => {
			const stops: Stop[] = [];
			gradients.push(stops);
			return { addColorStop: (offset: number, color: string) => stops.push([offset, color]) };
		})
	};
	context.stroke.mockImplementation(() => {
		strokes.push({ alpha: context.globalAlpha, width: context.lineWidth });
	});
	return { context: context as unknown as CanvasRenderingContext2D, gradients, strokes, calls: context };
}

describe('aurora painter', () => {
	it('draws two ribbons with the glow and edge gradients of the reference', () => {
		const { context, gradients, strokes, calls } = recordingContext(600, 450);
		const paint = createAuroraPainter(context);
		expect(calls.lineCap).toBe('round');
		expect(gradients).toEqual([
			[
				[0, '#178db6'],
				[0.48, '#24b8b7'],
				[1, '#65c4ad']
			],
			[
				[0, '#299fc7'],
				[0.42, '#54d0c0'],
				[0.75, '#a2d89e'],
				[1, '#79b6c7']
			]
		]);
		expect(calls.createLinearGradient).toHaveBeenNthCalledWith(1, 0, 0, 150, 337.5);
		expect(calls.createLinearGradient).toHaveBeenNthCalledWith(2, 0, 0, 0, 360);

		paint(0);
		expect(calls.clearRect).toHaveBeenCalledWith(0, 0, 600, 450);
		// One path per ribbon, through seven rows: a starting point and six
		// curve segments each.
		expect(calls.beginPath).toHaveBeenCalledTimes(2);
		expect(calls.moveTo).toHaveBeenCalledTimes(2);
		expect(calls.bezierCurveTo).toHaveBeenCalledTimes(12);
		// Five glow strokes and one edge stroke per ribbon. The fainter ribbon
		// scales every opacity by its strength (0.61).
		const opacities = [0.1, 0.12, 0.16, 0.24, 0.32, 0.52];
		expect(strokes.map(({ alpha }) => alpha)).toEqual(
			[...opacities.map((alpha) => alpha * 0.61), ...opacities].map((alpha) => expect.closeTo(alpha, 6))
		);
		// Widths scale with the short side (450) and the ribbon's size.
		expect(strokes.map(({ width }) => width)).toEqual([
			0.31 * 450 * 0.87, 0.22 * 450 * 0.87, 0.15 * 450 * 0.87, 0.1 * 450 * 0.87, 0.06 * 450 * 0.87,
			0.036 * 450 * 0.87,
			0.31 * 450, 0.22 * 450, 0.15 * 450, 0.1 * 450, 0.06 * 450, 0.036 * 450
		].map((width) => expect.closeTo(width, 6)));
	});

	it('moves the ribbons with the phase and loops without a jump', () => {
		const { context, calls } = recordingContext(800, 600);
		const paint = createAuroraPainter(context);
		const curveAt = (phase: number) => {
			vi.mocked(calls.bezierCurveTo).mockClear();
			paint(phase);
			return calls.bezierCurveTo.mock.calls;
		};
		// The second ribbon is shifted by 0.16 of a loop, so a seam carries a
		// floating-point residue far below a pixel; compare with a tolerance.
		const sameCurve = (actual: unknown[][], expected: unknown[][]) => {
			expect(actual).toHaveLength(expected.length);
			for (let segment = 0; segment < expected.length; segment++)
				for (let arg = 0; arg < expected[segment].length; arg++)
					expect(actual[segment][arg]).toBeCloseTo(expected[segment][arg] as number, 6);
		};
		const start = curveAt(0);
		const middle = curveAt(0.5);
		expect(middle).not.toEqual(start);
		// Every row stays a curve of the same shape: six segments per ribbon.
		expect(start).toHaveLength(12);
		expect(middle).toHaveLength(12);
		sameCurve(curveAt(1), start);
		// A phase beyond one loop is the same loop.
		sameCurve(curveAt(2.25), curveAt(0.25));
	});

	it('exposes the loop length the component paces its frames with', () => {
		expect(auroraCycleMs).toBe(7200);
	});
});
