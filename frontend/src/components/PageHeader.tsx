/**
 * PageHeader: the top-bar slot mechanism. Pages publish their filters,
 * search, stats, tabs, and primary actions into the shell's top bar instead
 * of rendering duplicate local toolbars. Registrations are scoped — the
 * scope string carries the account generation, the route, and the site id,
 * so switching pages, sites, or accounts invalidates stale entries even if
 * a cleanup runs late. Unmounting removes the entry via the layout-effect
 * cleanup.
 */
import {
	createContext,
	useContext,
	useId,
	useLayoutEffect,
	useMemo,
	useState,
	type ReactNode
} from 'react';
import { Segmented } from 'antd';

export interface PageHeaderContent {
	leading?: ReactNode;
	content?: ReactNode;
	actions?: ReactNode;
}

interface Registration {
	scope: string;
	publish: (owner: string, scope: string, value: PageHeaderContent) => () => void;
}

const RegistrationContext = createContext<Registration | null>(null);
const ContentContext = createContext<PageHeaderContent | undefined>(undefined);

export function PageHeaderProvider({ scope, children }: { scope: string; children: ReactNode }) {
	const [records, setRecords] = useState(
		new Map<string, { scope: string; value: PageHeaderContent }>()
	);

	const publish = (owner: string, expectedScope: string, value: PageHeaderContent) => {
		setRecords((current) => new Map(current).set(owner, { scope: expectedScope, value }));
		return () => {
			setRecords((current) => {
				if (current.get(owner)?.scope !== expectedScope) return current;
				const next = new Map(current);
				next.delete(owner);
				return next;
			});
		};
	};

	const registration = useMemo(() => ({ scope, publish }), [scope]);
	const value = [...records.values()]
		.filter((record) => record.scope === scope)
		.reduce<PageHeaderContent>((result, record) => ({ ...result, ...record.value }), {});

	return (
		<RegistrationContext.Provider value={registration}>
			<ContentContext.Provider value={value}>{children}</ContentContext.Provider>
		</RegistrationContext.Provider>
	);
}

/**
 * Publishes top-bar content for the calling page. Returns false when no
 * provider is mounted so the page can fall back to rendering locally.
 */
export function usePageHeader(value: PageHeaderContent, enabled = true): boolean {
	const registration = useContext(RegistrationContext);
	const owner = useId();
	const { leading, content, actions } = value;
	useLayoutEffect(() => {
		if (!enabled || !registration) return undefined;
		return registration.publish(owner, registration.scope, { leading, content, actions });
	}, [registration, owner, enabled, leading, content, actions]);
	return !!registration;
}

export function usePageHeaderContent(): PageHeaderContent | undefined {
	return useContext(ContentContext);
}

/** In-page switches render as top-bar segmented tabs, never a second tab bar. */
export function PageTabs({
	value,
	options,
	onChange,
	disabled,
	ariaLabel
}: {
	value: string;
	options: { label: ReactNode; value: string; disabled?: boolean }[];
	onChange: (value: string) => void;
	disabled?: boolean;
	ariaLabel?: string;
}) {
	return (
		<Segmented
			className="topbar-tabs"
			aria-label={ariaLabel ?? 'tabs'}
			value={value}
			options={options}
			disabled={disabled}
			onChange={(next) => onChange(String(next))}
		/>
	);
}
