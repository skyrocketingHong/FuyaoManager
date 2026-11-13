// InfoCard's title/stats template from robot-platform's sidebar/InfoCard.tsx.
export interface SidebarStat {
	label: string;
	value: string;
	machine?: boolean;
}

export default function SidebarInfo({ title, stats }: { title: string; stats: SidebarStat[] }) {
	return (
		<div className="sidebar-info">
			<section className="sidebar-info-card" aria-label={title}>
				<header className="info-head">
					<strong>{title}</strong>
				</header>
				<div className="info-foot">
					<dl className="info-stats">
						{stats.map((stat) => (
							<div className="scope-stat" key={stat.label}>
								<dt>{stat.label}</dt>
								<dd className={stat.machine ? 'version-text' : undefined}>{stat.value}</dd>
							</div>
						))}
					</dl>
				</div>
			</section>
		</div>
	);
}
