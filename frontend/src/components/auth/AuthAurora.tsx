/**
 * AuthAurora: the continuous aurora behind the sign-in and passkey pages
 * (robot-platform's modules/login/ui/src/LoginAurora.tsx, commit b336dd75).
 * A canvas covering the screen, drawn at a lower resolution and softened by
 * a CSS blur, shows two ribbons of light drifting through a 7.2-second loop.
 *
 * While `moving` is false the canvas shows the current frame and no frames
 * are requested. Playback also stops when the tab is hidden and continues
 * from the same point of the loop when it is shown again or motion resumes.
 * Without a 2D context nothing is drawn and the sky gradient alone shows.
 */
import { useEffect, useRef } from 'react';
import { auroraCycleMs, createAuroraPainter } from '../../lib/ui/aurora';

export default function AuthAurora({ moving }: { moving: boolean }) {
	const canvasRef = useRef<HTMLCanvasElement>(null);
	const elapsed = useRef(0);

	useEffect(() => {
		const canvas = canvasRef.current;
		const context = canvas?.getContext('2d');
		if (!canvas || !context) return;

		let frame = 0;
		let previousTime: number | undefined;
		let paint: ReturnType<typeof createAuroraPainter> | undefined;
		const tick = (now: number) => {
			if (previousTime !== undefined)
				elapsed.current = (elapsed.current + now - previousTime) % auroraCycleMs;
			previousTime = now;
			paint?.(elapsed.current / auroraCycleMs);
			frame = requestAnimationFrame(tick);
		};
		const syncPlayback = () => {
			cancelAnimationFrame(frame);
			previousTime = undefined;
			frame = moving && !document.hidden && paint ? requestAnimationFrame(tick) : 0;
		};
		// The blur hides fine detail, so the canvas needs at most 720px on its
		// long side (and never more than 0.75 of its size on screen).
		const observer = new ResizeObserver(([{ contentRect: { width, height } }]) => {
			if (width === 0 || height === 0) return;
			const scale = Math.min(0.75, 720 / Math.max(width, height));
			const pixelWidth = Math.max(1, Math.round(width * scale));
			const pixelHeight = Math.max(1, Math.round(height * scale));
			const firstPaint = paint === undefined;
			if (!firstPaint && canvas.width === pixelWidth && canvas.height === pixelHeight) return;
			canvas.width = pixelWidth;
			canvas.height = pixelHeight;
			paint = createAuroraPainter(context);
			paint(elapsed.current / auroraCycleMs);
			if (firstPaint) syncPlayback();
		});

		observer.observe(canvas);
		document.addEventListener('visibilitychange', syncPlayback);
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
			document.removeEventListener('visibilitychange', syncPlayback);
		};
	}, [moving]);

	return (
		<div className="auth-aurora">
			<canvas ref={canvasRef} />
		</div>
	);
}
