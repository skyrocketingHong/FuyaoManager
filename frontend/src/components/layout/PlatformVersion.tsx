import { useEffect, useState } from 'react';
import { version } from '../../../package.json';
import VersionBadge from '../VersionBadge';
import VersionText from '../VersionText';
import { api } from '../../lib/api/client';
import { t } from '../../lib/i18n';

type RuntimeVersion = { version: string; commit: string };

export default function PlatformVersion() {
	const [open, setOpen] = useState(false);
	const [runtime, setRuntime] = useState<RuntimeVersion>();
	const [failed, setFailed] = useState(false);
	useEffect(() => {
		if (!open || runtime) return;
		const controller = new AbortController();
		setFailed(false);
		void api<RuntimeVersion>('GET', '/api/v1/version', { signal: controller.signal })
			.then(({ data }) => {
				if (!controller.signal.aborted) setRuntime(data);
			})
			.catch(() => {
				if (!controller.signal.aborted) setFailed(true);
			});
		return () => controller.abort();
	}, [open, runtime]);

	return (
		<VersionBadge
			size="middle"
			label={t('updates.versionTag')}
			version={version}
			onOpenChange={setOpen}
			details={
				<div className="version-facts">
					<span>
						{t('updates.appLabel')} <VersionText>{version}</VersionText>
					</span>
					{runtime ? (
						<>
							<span>
								{t('updates.backendLabel')} <VersionText>{runtime.version}</VersionText>
							</span>
							<VersionText>{runtime.commit}</VersionText>
						</>
					) : (
						<span>{t(failed ? 'updates.versionUnknown' : 'common.loading')}</span>
					)}
				</div>
			}
		/>
	);
}
