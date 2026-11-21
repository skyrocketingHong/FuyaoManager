const reduceMotion = '(prefers-reduced-motion: reduce)';

export function staggerAlbumCards(track: HTMLElement) {
	if (
		typeof IntersectionObserver !== 'function' ||
		typeof track.animate !== 'function' ||
		typeof window.matchMedia !== 'function'
	)
		return;
	const motion = window.matchMedia(reduceMotion);
	if (motion.matches) return;

	// The static cards are already visible. Only intersecting cards enter a batch.
	const running = new Map<Element, Animation>();
	let observer: IntersectionObserver | undefined;
	const cleanup = () => {
		observer?.disconnect();
		for (const animation of running.values()) animation.cancel();
		running.clear();
		track.removeEventListener('focusin', onFocus);
		motion.removeEventListener('change', onPreferenceChange);
	};
	function onFocus(event: FocusEvent) {
		if (!(event.target instanceof Element)) return;
		const card = event.target.closest('.album-photo-tile');
		if (card) {
			running.get(card)?.cancel();
			running.delete(card);
		}
	}
	function onPreferenceChange() {
		if (motion.matches) cleanup();
	}

	try {
		observer = new IntersectionObserver(
			(entries) => {
				if (motion.matches) {
					cleanup();
					return;
				}
				const cards = entries.filter((entry) => entry.isIntersecting);
				try {
					for (const [i, { target }] of cards.entries()) {
						observer?.unobserve(target);
						if (target.contains(document.activeElement)) continue;
						const animation = target.animate(
							[
								{ opacity: 0, transform: 'translateY(16px)' },
								{ opacity: 1, transform: 'translateY(0)' }
							],
							{
								duration: 500,
								delay: 75 * i,
								easing: 'cubic-bezier(0.16, 1, 0.3, 1)',
								fill: 'backwards'
							}
						);
						running.set(target, animation);
						animation.onfinish = () => running.delete(target);
					}
				} catch {
					cleanup();
				}
			},
			{ rootMargin: '0px 0px 40px 0px' }
		);
		for (const card of track.children) observer.observe(card);
		track.addEventListener('focusin', onFocus);
		motion.addEventListener('change', onPreferenceChange);
	} catch {
		cleanup();
	}
	return cleanup;
}

export function mountAlbumHorizontalScroll(viewport: HTMLElement, track: HTMLElement) {
	if (typeof ResizeObserver !== 'function' || typeof window.matchMedia !== 'function') return;

	const motion = window.matchMedia(reduceMotion);
	let distance = 0;
	let frame = 0;
	let observer: ResizeObserver | undefined;

	const reset = () => {
		distance = 0;
		delete viewport.dataset.horizontalReady;
		viewport.removeAttribute('tabindex');
		viewport.style.removeProperty('--album-window-height');
		viewport.style.removeProperty('--album-runway-height');
		track.style.removeProperty('transform');
	};
	const clamp = (offset: number) => Math.min(distance, Math.max(0, offset));

	function moveTrack() {
		if (distance > 0 && !motion.matches) {
			// At 1× speed, one vertical pixel moves the track one horizontal pixel.
			track.style.transform = `translate3d(${-clamp(viewport.scrollTop)}px, 0, 0)`;
		}
	}
	function measure() {
		if (motion.matches) {
			reset();
			return;
		}
		viewport.dataset.horizontalReady = 'true';
		const height = viewport.clientHeight;
		distance = Math.max(0, track.scrollWidth - viewport.clientWidth);
		if (!height || !viewport.clientWidth || distance <= 1) {
			reset();
			return;
		}
		viewport.style.setProperty('--album-window-height', `${height}px`);
		viewport.style.setProperty('--album-runway-height', `${height + distance}px`);
		viewport.tabIndex = 0;
		moveTrack();
	}
	function scheduleMeasure() {
		if (frame) return;
		frame = window.requestAnimationFrame(() => {
			frame = 0;
			try {
				measure();
			} catch {
				cleanup();
			}
		});
	}
	function onKeyDown(event: KeyboardEvent) {
		if (
			event.target !== viewport ||
			!distance ||
			motion.matches ||
			event.altKey ||
			event.ctrlKey ||
			event.metaKey
		)
			return;
		const step = viewport.clientHeight * 0.8;
		const offsets: Partial<Record<string, number>> = {
			ArrowLeft: -step,
			ArrowUp: -step,
			PageUp: -step,
			ArrowRight: step,
			ArrowDown: step,
			PageDown: step,
			Home: -viewport.scrollTop,
			End: distance - viewport.scrollTop
		};
		const offset = offsets[event.key];
		if (offset === undefined) return;
		const next = clamp(viewport.scrollTop + offset);
		if (next === viewport.scrollTop) return;
		event.preventDefault();
		viewport.scrollTop = next;
		moveTrack();
	}
	function onFocus(event: FocusEvent) {
		if (
			!distance ||
			motion.matches ||
			!(event.target instanceof HTMLElement) ||
			!track.contains(event.target)
		)
			return;
		const focus = event.target.getBoundingClientRect();
		const bounds = viewport.getBoundingClientRect();
		const delta =
			focus.left < bounds.left
				? focus.left - bounds.left
				: Math.max(0, focus.right - bounds.left - viewport.clientWidth);
		if (!delta) return;
		viewport.scrollTop = clamp(viewport.scrollTop + delta);
		moveTrack();
	}
	function cleanup() {
		if (frame) window.cancelAnimationFrame(frame);
		observer?.disconnect();
		motion.removeEventListener('change', scheduleMeasure);
		viewport.removeEventListener('scroll', moveTrack);
		viewport.removeEventListener('keydown', onKeyDown);
		viewport.removeEventListener('focusin', onFocus);
		reset();
	}

	try {
		observer = new ResizeObserver(scheduleMeasure);
		observer.observe(viewport);
		observer.observe(track);
		viewport.addEventListener('scroll', moveTrack, { passive: true });
		viewport.addEventListener('keydown', onKeyDown);
		viewport.addEventListener('focusin', onFocus);
		motion.addEventListener('change', scheduleMeasure);
		measure();
	} catch {
		cleanup();
	}
	return cleanup;
}
