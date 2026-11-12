/**
 * SidebarNav: the generic menu host — items, selection, and navigation all
 * arrive from the navigation metadata consumer, so this component knows
 * nothing about page ownership. Only the menu region scrolls; the fixed
 * action region stays outside it.
 * Ported from the robot platform's components/platform/sidebar/SidebarNav.tsx
 * (read-only reference), adapted to antd 6 and this console's i18n.
 */
import { Menu } from 'antd';
import type { MenuProps } from 'antd';

export interface SidebarNavModule {
	items: Required<MenuProps>['items'];
	selectedKeys: string[];
	onSelect: (key: string) => void;
	ariaLabel: string;
}

export default function SidebarNav({ module }: { module: SidebarNavModule }) {
	return (
		<nav aria-label={module.ariaLabel}>
			<Menu
				theme="light"
				mode="inline"
				inlineIndent={0}
				items={module.items}
				selectedKeys={module.selectedKeys}
				onClick={({ key }) => module.onSelect(String(key))}
			/>
		</nav>
	);
}
