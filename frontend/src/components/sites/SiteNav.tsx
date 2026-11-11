/**
 * Shared site sub-navigation: one antd Navigation whose items mirror the
 * route hierarchy under /sites/:siteId.
 */
import { NavLink, useParams } from 'react-router-dom';
import { Menu } from 'antd';
import type { MenuProps } from 'antd';
import { t, type MessageKey } from '../../lib/i18n';

const items: { key: string; path: string; labelKey: MessageKey }[] = [
	{ key: 'overview', path: '', labelKey: 'nav.overview' },
	{ key: 'settings', path: '/settings', labelKey: 'nav.settings' },
	{ key: 'theme', path: '/theme', labelKey: 'nav.theme' },
	{ key: 'builds', path: '/builds', labelKey: 'nav.builds' },
	{ key: 'releases', path: '/releases', labelKey: 'nav.releases' }
];

export default function SiteNav({ active }: { active: string }) {
	const { siteId } = useParams<{ siteId: string }>();
	const menuItems: MenuProps['items'] = items.map((item) => ({
		key: item.key,
		label: <NavLink to={`/sites/${siteId}${item.path}`}>{t(item.labelKey)}</NavLink>
	}));
	return <Menu mode="horizontal" selectedKeys={[active]} items={menuItems} />;
}
