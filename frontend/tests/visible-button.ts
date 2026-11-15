import { expect } from 'vitest';
import { within } from '@testing-library/react';

// Match Testing Library's visibility checks while computing the button's
// styles once; jsdom can spend seconds recomputing antd table button styles.
export function getVisibleButton(container: HTMLElement, name: string): HTMLElement {
	const button = within(container).getByRole('button', { name, hidden: true });
	const buttonStyle = window.getComputedStyle(button);
	expect(buttonStyle.visibility).not.toBe('hidden');
	for (let element: HTMLElement | null = button; element; element = element.parentElement) {
		expect(element.hidden).toBe(false);
		expect(element.getAttribute('aria-hidden')).not.toBe('true');
		const style = element === button ? buttonStyle : window.getComputedStyle(element);
		expect(style.display).not.toBe('none');
	}
	return button;
}
