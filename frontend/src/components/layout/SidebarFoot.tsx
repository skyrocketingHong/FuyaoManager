// Footer layout from robot-platform's
// frontend/components/platform/sidebar/SidebarFoot.tsx: a rule, then a row on
// the sidebar's two alignment lines — the project link on the icon axis and
// the version on the text axis.
import { siGithub } from 'simple-icons';
import { version } from '../../../package.json';

export default function SidebarFoot() {
	return (
		<div className="sidebar-foot">
			<a
				className="sidebar-foot-link"
				href="https://github.com/skyrocketingHong/FuyaoManager"
				target="_blank"
				rel="noreferrer"
				aria-label="FuyaoManager on GitHub"
			>
				<svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
					<path d={siGithub.path} />
				</svg>
			</a>
			<code className="sidebar-foot-version">{version}</code>
		</div>
	);
}
