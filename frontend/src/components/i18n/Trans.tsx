/**
 * Trans: fades static text in when the locale switches, mirroring the
 * public site's Crossfade convention. Only the text node re-keys — wrapping
 * form controls would remount them and lose input or focus. The animation
 * is disabled entirely under prefers-reduced-motion via app.css.
 */
import { useSyncExternalStore } from 'react';
import { i18nSnapshot, subscribeI18n } from '../../lib/i18n';

export default function Trans({ text }: { text: string }) {
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	return (
		<span key={locale} className="i18n-fade">
			{text}
		</span>
	);
}
