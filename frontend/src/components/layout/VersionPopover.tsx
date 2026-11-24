/**
 * VersionPopover: the top bar's version capsule and the details panel it opens
 * (robot-platform's libraries/ui/operationbar/StatusPopover.tsx without its
 * device capsule effects). The panel has no arrow and no antd motion; its
 * surface morphs from the capsule's size to the details' size, and back to the
 * capsule when it closes, so the two read as one object. The morph can be
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
import { animatePillSize, type PillSize } from '../../lib/ui/pillMotion';

function reducedMotion() {
	return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
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
	const initial = useRef<PillSize>(undefined);
	const resize = useRef<Animation>(undefined);
	const fade = useRef<Animation>(undefined);
	const [closing, setClosing] = useState(false);

	const cancel = useCallback(() => {
		resize.current?.cancel();
		fade.current?.cancel();
		resize.current = fade.current = undefined;
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

	useLayoutEffect(() => {
		if (!open || !panel || !surface) return;
		let measured: PillSize | undefined;
		// The portal can still be hidden when its ref attaches, and the details
		// change size once they load. Each measurable size morphs from the size
		// shown at that moment: the capsule for the entrance, the current
		// surface for a resize in flight.
		const measure = () => {
			const size = panel.getBoundingClientRect();
			const starting = !measured;
			if (
				!size.width ||
				!size.height ||
				(!starting &&
					(!resize.current ||
						(measured?.width === size.width && measured.height === size.height)))
			)
				return;
			measured = size;
			const from = starting
				? (initial.current ?? trigger.current?.getBoundingClientRect())
				: surface.getBoundingClientRect();
			initial.current = undefined;
			resize.current?.cancel();
			const animation = animatePillSize(surface, from, surface.getBoundingClientRect(), reducedMotion());
			resize.current = animation;
			if (animation) {
				const finish = () => {
					if (resize.current === animation) resize.current = undefined;
				};
				void animation.finished.then(finish, finish);
			}
			if (starting) {
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
		if (typeof ResizeObserver !== 'function') return;
		const observer = new ResizeObserver(measure);
		observer.observe(panel);
		return () => observer.disconnect();
	}, [open, panel, surface]);

	useLayoutEffect(() => {
		if (!open && !closing) cancel();
	}, [open, closing, cancel]);

	// React 18 writes `inert="false"` as a present attribute, so the property
	// is set here instead of passing it as a prop.
	useEffect(() => {
		if (panel) panel.inert = !open;
	}, [open, panel]);

	function changeOpen(next: boolean) {
		if (next === open) return;
		if (next) {
			initial.current = (closing ? (surface ?? trigger.current) : trigger.current)?.getBoundingClientRect();
			cancel();
			setClosing(false);
		} else if (surface) {
			const from = surface.getBoundingClientRect();
			cancel();
			const animation = animatePillSize(
				surface,
				from,
				trigger.current?.getBoundingClientRect(),
				reducedMotion(),
				true
			);
			if (animation) {
				resize.current = animation;
				setClosing(true);
				const finish = () => {
					if (resize.current !== animation) return;
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
