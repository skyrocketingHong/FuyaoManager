/**
 * VersionPopover: the top bar's version capsule and the details panel it opens
 * (robot-platform's libraries/ui/operationbar/StatusPopover.tsx without its
 * device capsule effects). The panel has no arrow and no antd motion; its
 * surface morphs from the capsule's position and size to its final placement,
 * and back to the capsule when it closes. The morph can be
 * interrupted from either state, and reduced motion or a browser without the
 * Web Animations API shows the final size directly.
 *
 * While closing, the panel stays mounted for the morph back but is inert, so
 * nothing inside takes focus. Escape closes the panel and returns focus to
 * the capsule. The version badges inside tables keep the plain popover.
 */
import { Button, Popover } from 'antd';
import {
	useCallback,
	useEffect,
	useId,
	useLayoutEffect,
	useRef,
	useState,
	type KeyboardEvent,
	type ReactNode
} from 'react';
import { animatePillBounds, rebasePillBounds, samePillBounds, type PillBounds } from '../../lib/ui/pillMotion';

function reducedMotion() {
	return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

function hasPopupLayout(rect: DOMRect) {
	return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0;
}

export default function VersionPopover({
	open,
	onOpenChange,
	label,
	children,
	content
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	label: string;
	children: ReactNode;
	content: ReactNode;
}) {
	const id = useId();
	const trigger = useRef<HTMLButtonElement>(null);
	const [panel, setPanel] = useState<HTMLElement | null>(null);
	const surface = panel?.closest<HTMLElement>('.version-popover-surface');
	const popup = surface?.closest<HTMLElement>('.version-popover');
	const initial = useRef<PillBounds>(undefined);
	const morph = useRef<Animation>(undefined);
	const fade = useRef<Animation>(undefined);
	const [closing, setClosing] = useState(false);

	const cancel = useCallback(() => {
		morph.current?.cancel();
		fade.current?.cancel();
		morph.current = fade.current = undefined;
	}, []);

	// Reduced motion switched on mid-morph ends the morph where it is.
	useEffect(() => {
		const preference =
			typeof matchMedia === 'function' ? matchMedia('(prefers-reduced-motion: reduce)') : undefined;
		const onPreference = () => {
			if (preference?.matches) {
				cancel();
				setClosing(false);
			}
		};
		preference?.addEventListener?.('change', onPreference);
		return () => {
			cancel();
			preference?.removeEventListener?.('change', onPreference);
		};
	}, [cancel]);

	// React 18 writes `inert="false"` as a present attribute, so the property
	// is set here before the measurement effect can move focus into the panel.
	useLayoutEffect(() => {
		if (panel) panel.inert = !open;
	}, [open, panel]);

	useLayoutEffect(() => {
		if (!open || !panel || !surface || !popup) return;
		let previousLayout: PillBounds | undefined;
		// The portal can still be hidden when its ref attaches, and the details
		// can change size once they load. With no arrow, the outer placement
		// box matches the surface's layout and is unaffected by its transform.
		const measure = () => {
			const layout = popup.getBoundingClientRect();
			if (!hasPopupLayout(layout)) return;
			const previous = previousLayout;
			if (previous && (!morph.current || samePillBounds(previous, layout))) return;
			previousLayout = layout;
			const from = previous
				? rebasePillBounds(surface.getBoundingClientRect(), previous, layout)
				: (initial.current ?? trigger.current?.getBoundingClientRect());
			initial.current = undefined;
			morph.current?.cancel();
			const animation = animatePillBounds(surface, from, layout, layout, reducedMotion());
			morph.current = animation;
			if (animation) {
				const finish = () => {
					if (morph.current === animation) morph.current = undefined;
				};
				void animation.finished.then(finish, finish);
			}
			if (!previous) {
				panel.focus({ preventScroll: true });
				if (animation) {
					try {
						fade.current = panel.firstElementChild?.animate([{ opacity: 0.35 }, { opacity: 1 }], {
							duration: 210,
							easing: 'ease-out',
							fill: 'backwards'
						});
					} catch {
						/* The details stay visible without the fade. */
					}
				}
			}
		};
		measure();
		const resizeObserver = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : undefined;
		resizeObserver?.observe(popup);
		const placementObserver = new MutationObserver(measure);
		placementObserver.observe(popup, { attributes: true, attributeFilter: ['style'] });
		return () => {
			resizeObserver?.disconnect();
			placementObserver.disconnect();
		};
	}, [open, panel, popup, surface]);

	useLayoutEffect(() => {
		if (!open && !closing) cancel();
	}, [open, closing, cancel]);

	function changeOpen(next: boolean) {
		if (next === open) return;
		if (next) {
			initial.current = (closing ? (surface ?? trigger.current) : trigger.current)?.getBoundingClientRect();
			cancel();
			setClosing(false);
		} else if (surface && popup) {
			const layout = popup.getBoundingClientRect();
			const from = surface.getBoundingClientRect();
			cancel();
			const animation = hasPopupLayout(layout)
				? animatePillBounds(surface, from, trigger.current?.getBoundingClientRect(), layout, reducedMotion(), true)
				: undefined;
			if (animation) {
				morph.current = animation;
				setClosing(true);
				const finish = () => {
					if (morph.current !== animation) return;
					setClosing(false);
				};
				void animation.finished.then(finish, finish);
			}
		}
		onOpenChange(next);
	}

	function onEscapeKeyDown(event: KeyboardEvent<HTMLElement>) {
		if (open && event.key === 'Escape' && !event.defaultPrevented) {
			event.stopPropagation();
			closeAndFocus();
		}
	}

	function closeAndFocus() {
		changeOpen(false);
		trigger.current?.focus({ preventScroll: true });
	}

	return (
		<Popover
			open={open || closing}
			onOpenChange={changeOpen}
			trigger="click"
			placement="bottomRight"
			fresh
			arrow={false}
			motion={{ motionName: '' }}
			classNames={{ root: 'version-popover', container: 'version-popover-surface' }}
			content={
				<section
					ref={setPanel}
					role="dialog"
					tabIndex={-1}
					className="version-popover-content"
					id={id}
					aria-label={label}
					onKeyDown={onEscapeKeyDown}
				>
					<div>{content}</div>
				</section>
			}
		>
			<Button
				ref={trigger}
				type="text"
				size="middle"
				className="version-badge topbar-version"
				title={label}
				aria-label={label}
				aria-haspopup="dialog"
				aria-expanded={open}
				aria-controls={open ? id : undefined}
				onClick={() => {
					if (closing) changeOpen(true);
				}}
				onKeyDown={onEscapeKeyDown}
			>
				{children}
			</Button>
		</Popover>
	);
}
