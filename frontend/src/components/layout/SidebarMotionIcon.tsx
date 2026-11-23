import { useEffect, useId, useRef, type CSSProperties } from 'react';

export type SidebarMotionIconName = 'posts' | 'albums' | 'footprints' | 'friends';

type MotionLayers = Record<string, readonly string[]>;
const tracks: Record<SidebarMotionIconName, MotionLayers> = {
	posts: {
		heading: ['scaleX(.48)', 'none', 'none', 'none'],
		firstLine: ['none', 'scaleX(.42)', 'none', 'none'],
		secondLine: ['none', 'none', 'scaleX(.38)', 'none']
	},
	albums: {
		back: ['translate(-.8px, -.8px)', 'none'],
		front: ['translate(.8px, .65px)', 'none']
	},
	footprints: {
		pin: ['translateY(-.85px)', 'translateY(.6px) scaleY(.88)', 'none', 'none']
	},
	friends: {
		back: ['translateX(-.7px)', 'translateX(.55px)', 'none'],
		front: ['translateX(.7px)', 'translateX(-.55px)', 'none']
	}
};

const reducedMotion = '(prefers-reduced-motion: reduce)';
const hovering = new WeakSet<SVGSVGElement>();
const motionLayer = '[data-motion-layer]';
const body = 'var(--sidebar-icon-body)';
const detail = 'var(--sidebar-icon-detail)';
const pageOrigin = { transformOrigin: '4.75px 0' };
const photo = 'M4 9.1C4 7.614 4 6.672 4.586 6.086C5.172 5.5 6.114 5.5 7.6 5.5L11.4 5.5C12.886 5.5 13.828 5.5 14.414 6.086C15 6.672 15 7.614 15 9.1L15 10.4C15 11.886 15 12.828 14.414 13.414C13.828 14 12.886 14 11.4 14L7.6 14C6.114 14 5.172 14 4.586 13.414C4 12.828 4 11.886 4 10.4Z';
const frontLink = 'M7 8.25C7 6.731 8.231 5.5 9.75 5.5L11.25 5.5C12.769 5.5 14 6.731 14 8.25L14 8.25C14 9.769 12.769 11 11.25 11L9.75 11C8.231 11 7 9.769 7 8.25Z';

function retarget(svg: SVGSVGElement, moves?: MotionLayers) {
	if (typeof svg.animate !== 'function' || typeof svg.getAnimations !== 'function') return;
	const layers = [...svg.querySelectorAll<SVGGElement>(motionLayer)].map((node) => {
		const running = node.getAnimations();
		return { node, running, start: running.length ? getComputedStyle(node).transform || 'none' : 'none' };
	});
	for (const { node, running, start } of layers) {
		for (const animation of running) animation.cancel();
		const targets = moves?.[node.dataset.motionLayer ?? ''] ?? (running.length ? ['none'] : null);
		if (!targets) continue;
		node.animate(
			[start, ...targets].map((transform) => ({ transform, easing: 'ease-in-out' })),
			{ duration: moves ? 300 : 150 }
		);
	}
}

function play(svg: SVGSVGElement, name: SidebarMotionIconName, fromHover: boolean) {
	if (window.matchMedia(reducedMotion).matches) return;
	if (fromHover) hovering.add(svg);
	else hovering.delete(svg);
	retarget(svg, tracks[name]);
}

export function replaySidebarMotionIcon(row: HTMLElement, name: SidebarMotionIconName) {
	const svg = row.querySelector<SVGSVGElement>('svg[data-sidebar-motion-icon]');
	if (svg) play(svg, name, false);
}

function artwork(name: SidebarMotionIconName, id: string) {
	const gap = `${id}-gap`;
	const photoClip = `${id}-photo`;
	switch (name) {
		case 'posts':
			return (
				<>
					<path fill={body} d="M2 4.6C2 3.114 2 2.172 2.586 1.586C3.172 1 4.114 1 5.6 1L10.4 1C11.886 1 12.828 1 13.414 1.586C14 2.172 14 3.114 14 4.6L14 11.4C14 12.886 14 13.828 13.414 14.414C12.828 15 11.886 15 10.4 15L5.6 15C4.114 15 3.172 15 2.586 14.414C2 13.828 2 12.886 2 11.4Z" />
					<g data-motion-layer="heading" style={pageOrigin}>
						<path fill={detail} d="M4.75 5.25C4.75 5.057 4.75 4.586 5.043 4.293C5.336 4 5.807 4 6 4L10 4C10.193 4 10.664 4 10.957 4.293C11.25 4.586 11.25 5.057 11.25 5.25C11.25 5.443 11.25 5.914 10.957 6.207C10.664 6.5 10.193 6.5 10 6.5L6 6.5C5.807 6.5 5.336 6.5 5.043 6.207C4.75 5.914 4.75 5.443 4.75 5.25Z" />
					</g>
					<g data-motion-layer="firstLine" style={pageOrigin}>
						<rect x="4.75" y="8" width="6.5" height="1.25" rx=".625" fill={detail} />
					</g>
					<g data-motion-layer="secondLine" style={pageOrigin}>
						<rect x="4.75" y="10.75" width="4.5" height="1.25" rx=".625" fill={detail} />
					</g>
				</>
			);
		case 'albums':
			return (
				<>
					<defs>
						<mask id={gap} maskUnits="userSpaceOnUse" x="-2" y="-2" width="20" height="20">
							<rect x="-2" y="-2" width="20" height="20" fill="white" />
							<g data-motion-layer="front">
								<path d={photo} fill="black" stroke="black" strokeWidth="2" strokeLinejoin="round" />
							</g>
						</mask>
						<clipPath id={photoClip}><path d={photo} /></clipPath>
					</defs>
					<g mask={`url(#${gap})`}>
						<g data-motion-layer="back">
							<path fill={detail} d="M1 5.6C1 4.114 1 3.172 1.586 2.586C2.172 2 3.114 2 4.6 2L7.15 2C8.636 2 9.578 2 10.164 2.586C10.75 3.172 10.75 4.114 10.75 5.6L10.75 6.15C10.75 7.636 10.75 8.578 10.164 9.164C9.578 9.75 8.636 9.75 7.15 9.75L4.6 9.75C3.114 9.75 2.172 9.75 1.586 9.164C1 8.578 1 7.636 1 6.15Z" />
						</g>
					</g>
					<g data-motion-layer="front">
						<path d={photo} fill={body} />
						<circle cx="7.75" cy="15.5" r="4.75" clipPath={`url(#${photoClip})`} fill={detail} />
						<circle cx="11.5" cy="9" r="1.25" fill={detail} />
					</g>
				</>
			);
		case 'footprints':
			return (
				<g data-motion-layer="pin" style={{ transformOrigin: '8px 14.75px' }}>
					<path fill={body} d="M2 6.612C2 3.513 4.513 1 7.612 1L8.388 1C11.487 1 14 3.513 14 6.612C14 7.027 13.868 7.43 13.623 7.765L8.799 14.358C8.469 14.809 8.279 15.069 8 15.069C7.721 15.069 7.531 14.809 7.201 14.358L2.377 7.765C2.132 7.43 2 7.027 2 6.612Z" />
					<circle cx="8" cy="6.25" r="2.125" fill={detail} />
				</g>
			);
		case 'friends':
			return (
				<>
					<defs>
						<mask id={gap} maskUnits="userSpaceOnUse" x="-2" y="-2" width="20" height="20">
							<rect x="-2" y="-2" width="20" height="20" fill="white" />
							<g data-motion-layer="front">
								<path d={frontLink} fill="none" stroke="black" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />
							</g>
						</mask>
					</defs>
					<g mask={`url(#${gap})`}>
						<g data-motion-layer="back">
							<path d="M8.25 5H4.75A2.75 2.75 0 0 0 4.75 10.5H8.25" fill="none" stroke={detail} strokeWidth="2" strokeLinecap="round" />
						</g>
					</g>
					<g data-motion-layer="front">
						<path d={frontLink} fill="none" stroke={body} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
					</g>
				</>
			);
	}
}

export default function SidebarMotionIcon({ name, className, style }: {
	name: SidebarMotionIconName;
	className?: string;
	style?: CSSProperties;
}) {
	const id = useId();
	const ref = useRef<SVGSVGElement>(null);
	useEffect(() => {
		const svg = ref.current;
		if (!svg) return;
		const preference = window.matchMedia(reducedMotion);
		const reset = () => {
			hovering.delete(svg);
			for (const node of svg.querySelectorAll(motionLayer)) {
				for (const animation of node.getAnimations?.() ?? []) animation.cancel();
			}
		};
		const onChange = (event: MediaQueryListEvent) => { if (event.matches) reset(); };
		preference.addEventListener('change', onChange);
		return () => {
			preference.removeEventListener('change', onChange);
			reset();
		};
	}, []);

	return (
		<span
			className={className}
			style={style}
			aria-hidden="true"
			onPointerEnter={(event) => {
				if (event.pointerType === 'mouse' && ref.current) play(ref.current, name, true);
			}}
			onPointerLeave={(event) => {
				if (event.pointerType === 'mouse' && ref.current && hovering.delete(ref.current)) retarget(ref.current);
			}}
		>
			<svg
				ref={ref}
				data-sidebar-motion-icon={name}
				width="16"
				height="16"
				viewBox="0 0 16 16"
				focusable="false"
				style={{ display: 'block', pointerEvents: 'none' }}
			>
				{artwork(name, id)}
			</svg>
		</span>
	);
}
