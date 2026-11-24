// Version/footer layout adapted from robot-platform's
// frontend/components/platform/sidebar/SidebarFoot.tsx.
import { siGithub } from 'simple-icons';
import { version } from '../../../package.json';

export default function SidebarFoot() {
	return (
		<div className="sidebar-foot">
			<div className="version-line">
				<code>{version}</code>
				<a
					href="https://github.com/skyrocketingHong/FuyaoManager"
					target="_blank"
					rel="noreferrer"
					aria-label="FuyaoManager on GitHub"
				>
					<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
						<path d={siGithub.path} />
					</svg>
				</a>
			</div>
		</div>
	);
}
