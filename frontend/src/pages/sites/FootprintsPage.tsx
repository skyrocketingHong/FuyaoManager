/**
 * FootprintsPage: the site's footprint points (cities and their places) as
 * one validated document. Editing uses plain coordinate inputs plus an
 * optional AMap picker (the site's own browser key); the picker reports
 * key/whitelist problems accurately instead of pretending the map works.
 * Stored coordinates are GCJ-02 — exactly what the theme's AMap view
 * consumes, so no conversion happens anywhere.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSyncExternalStore } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	Button,
	Card,
	Collapse,
	Input,
	InputNumber,
	Popconfirm,
	Space,
	message
} from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import { Panel } from '../../components/Panel';
import { useContentScope } from '../../components/ContentScopeProvider';
import {
	getFootprints,
	getSite,
	getSiteConfig,
	putFootprints,
	type FootprintCity,
	type FootprintPlace,
	type Site
} from '../../lib/api/management';
import { EditableDoc } from '../../lib/editing/EditableDoc';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

interface AMapInstance {
	on: (
		event: string,
		cb: (e: { lnglat: { getLng: () => number; getLat: () => number } }) => void
	) => void;
}

interface AMapLike {
	Map: new (
		el: HTMLElement,
		opts?: Record<string, unknown>
	) => {
		on: (
			event: string,
			cb: (e: { lnglat: { getLng: () => number; getLat: () => number } }) => void
		) => void;
	};
}

declare global {
	interface Window {
		AMap?: AMapLike;
		_AMapSecurityConfig?: { serviceHost?: string; securityJsCode?: string };
	}
}

// loadAMap injects the AMap script once per key.
const amapLoads = new Map<string, Promise<AMapLike | null>>();
function loadAMap(key: string, serviceHost?: string): Promise<AMapLike | null> {
	if (!key) return Promise.resolve(null);
	const cached = amapLoads.get(key);
	if (cached) return cached;
	const load = new Promise<AMapLike | null>((resolve) => {
		if (serviceHost) {
			window._AMapSecurityConfig = { serviceHost };
		}
		const existing = document.querySelector<HTMLScriptElement>('script[data-amap-key]');
		const settle = () => {
			if (window.AMap) resolve(window.AMap);
			else resolve(null);
		};
		if (existing) {
			existing.addEventListener('load', settle);
			existing.addEventListener('error', () => resolve(null));
			if (window.AMap) settle();
			return;
		}
		const script = document.createElement('script');
		script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(key)}`;
		script.async = true;
		script.dataset.amapKey = key;
		script.addEventListener('load', settle);
		script.addEventListener('error', () => resolve(null));
		document.head.appendChild(script);
	});
	amapLoads.set(key, load);
	return load;
}

export default function FootprintsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const editor = useMemo(() => new EditableDoc<{ cities: FootprintCity[] }>(), [scope.key]);
	const state = useSyncExternalStore(editor.subscribe, editor.getSnapshot);
	const dirty = editor.isDirty();
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [amapKey, setAmapKey] = useState('');
	const [amapServiceHost, setAmapServiceHost] = useState('');
	const effectiveAmapKey = scope.amap ? (scope.amap.browserKey ?? '') : amapKey;
	const effectiveAmapServiceHost = scope.amap ? (scope.amap.serviceHost ?? '') : amapServiceHost;
	const [mapState, setMapState] = useState<'idle' | 'ready' | 'failed'>('idle');
	const [pickedFor, setPickedFor] = useState<{
		kind: 'city' | 'place';
		cityIndex: number;
		placeIndex?: number;
	} | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	const mapHostRef = useRef<HTMLDivElement | null>(null);
	const mapRef = useRef<AMapInstance | null>(null);
	const epoch = useRef(0);

	const load = useCallback(async () => {
		if (!target) return;
		const my = ++epoch.current;
		editor.markLoading();
		try {
			const env = await getFootprints(target);
			if (my !== epoch.current) return;
			editor.load(env.footprints, env.digest);
			setErrorKey(null);
		} catch (err) {
			if (my !== epoch.current) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		}
	}, [target, navigate, editor]);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		setSite(null);
		let cancelled = false;
		if (siteId && scope.canEdit === undefined)
			getSite(siteId)
				.then((s) => {
					if (!cancelled) setSite(s);
				})
				.catch(() => {});
		if (siteId && !scope.amap)
			getSiteConfig(siteId)
				.then((env) => {
					if (cancelled) return;
					setAmapKey(env.config.services?.amap?.browserKey ?? '');
					setAmapServiceHost(env.config.services?.amap?.serviceHost ?? '');
				})
				.catch(() => {});
		void load();
		return () => {
			epoch.current++;
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [scope.key]);

	// Initialize the picker map when a target is selected.
	useEffect(() => {
		if (!pickedFor) return;
		let cancelled = false;
		void loadAMap(effectiveAmapKey, effectiveAmapServiceHost).then((AMap) => {
			if (cancelled || !AMap || !mapHostRef.current) {
				if (!cancelled && !AMap) setMapState('failed');
				return;
			}
			try {
				const map = new AMap.Map(mapHostRef.current, { zoom: 11, center: [116.397, 39.909] });
				map.on('click', (e) => {
					const lng = e.lnglat.getLng();
					const lat = e.lnglat.getLat();
					applyPick(lng, lat);
				});
				mapRef.current = map;
				setMapState('ready');
			} catch {
				setMapState('failed');
			}
		});
		return () => {
			cancelled = true;
			mapRef.current = null;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [pickedFor, effectiveAmapKey, effectiveAmapServiceHost]);

	function applyPick(lng: number, lat: number) {
		if (!pickedFor) return;
		const current = state.draft ?? state.baseline;
		if (!current) return;
		const cities = current.cities.map((city, ci) => {
			if (pickedFor.kind === 'city' && ci === pickedFor.cityIndex) {
				return { ...city, coordinates: { lng, lat } };
			}
			if (pickedFor.kind === 'place' && ci === pickedFor.cityIndex) {
				return {
					...city,
					places: (city.places ?? []).map((place, pi) =>
						pi === pickedFor.placeIndex ? { ...place, coordinates: { lng, lat } } : place
					)
				};
			}
			return city;
		});
		editor.setDraft({ cities });
	}

	function mutate(updater: (cities: FootprintCity[]) => FootprintCity[]) {
		editor.mutate((current) => ({ cities: updater(current.cities) }));
	}

	function updateCity(index: number, patch: Partial<FootprintCity>) {
		mutate((cities) => cities.map((c, i) => (i === index ? { ...c, ...patch } : c)));
	}

	function updatePlace(cityIndex: number, placeIndex: number, patch: Partial<FootprintPlace>) {
		mutate((cities) =>
			cities.map((c, ci) =>
				ci === cityIndex
					? {
							...c,
							places: (c.places ?? []).map((p, pi) => (pi === placeIndex ? { ...p, ...patch } : p))
						}
					: c
			)
		);
	}

	function addCity() {
		mutate((cities) => [
			...cities,
			{
				id: `city-${Date.now().toString(36)}`,
				name: { zh: '', en: '' },
				coordinates: { lng: 116.407526, lat: 39.90403 },
				type: 'city',
				visitDate: new Date().toISOString().slice(0, 10).replace(/-/g, ''),
				places: []
			}
		]);
	}

	function addPlace(cityIndex: number) {
		mutate((cities) =>
			cities.map((c, ci) =>
				ci === cityIndex
					? {
							...c,
							places: [
								...(c.places ?? []),
								{
									id: `${c.id}-${(c.places?.length ?? 0) + 1}`,
									name: { zh: '', en: '' },
									coordinates: c.coordinates,
									type: 'spot',
									visitDate: c.visitDate
								}
							]
						}
					: c
			)
		);
	}

	async function handleSave() {
		if (!target) return;
		const snapshot = editor.beginSave();
		if (snapshot === null) return;
		try {
			const saved = await putFootprints(target, snapshot, state.etag);
			editor.resolveSave(saved.footprints, saved.digest);
			void messageApi.success(t('footprints.saved'));
		} catch (err) {
			if (apiErrorKey(err) === 'error.conflict') {
				editor.rejectSave(true);
			} else {
				editor.rejectSave(false);
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
			}
		}
	}

	// Conflict sync adopts the server baseline + ETag and keeps the draft.
	async function syncAfterConflict() {
		if (!target) return;
		const my = epoch.current;
		try {
			const env = await getFootprints(target);
			if (my !== epoch.current) return;
			editor.syncAfterConflict(env.footprints, env.digest);
			void messageApi.info(t('friends.conflictSynced'));
		} catch {
			// keep the banner
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	useUnsavedGuard(dirty);

	function coordInputs(
		value: { lng: number; lat: number },
		onChange: (coord: { lng: number; lat: number }) => void,
		pickTarget: { kind: 'city' | 'place'; cityIndex: number; placeIndex?: number }
	) {
		return (
			<Space wrap>
				<InputNumber
					size="small"
					style={{ width: 130 }}
					addonBefore="lng"
					value={value.lng}
					disabled={!editable}
					step={0.000001}
					onChange={(v) => onChange({ ...value, lng: Number(v) })}
				/>
				<InputNumber
					size="small"
					style={{ width: 130 }}
					addonBefore="lat"
					value={value.lat}
					disabled={!editable}
					step={0.000001}
					onChange={(v) => onChange({ ...value, lat: Number(v) })}
				/>
				{editable ? (
					<Button
						size="small"
						onClick={() =>
							setPickedFor((prev) =>
								prev && prev.cityIndex === pickTarget.cityIndex ? null : pickTarget
							)
						}
					>
						{pickedFor?.cityIndex === pickTarget.cityIndex &&
						pickedFor?.placeIndex === pickTarget.placeIndex
							? t('footprints.picking')
							: t('footprints.pick')}
					</Button>
				) : null}
			</Space>
		);
	}

	const headerActions = (
		<Space size={8}>
			{state.conflict ? (
				<>
					<Button onClick={() => void syncAfterConflict()}>{t('posts.conflictAction')}</Button>
					<Button danger onClick={() => editor.discardLocal()}>
						{t('posts.discardLocal')}
					</Button>
				</>
			) : null}
			{editable ? <Button onClick={addCity}>{t('footprints.addCity')}</Button> : null}
			<Button
				type="primary"
				loading={state.saving}
				disabled={!editable || !dirty}
				onClick={() => void handleSave()}
			>
				<Trans text={t('common.save')} />
			</Button>
		</Space>
	);
	const headerActive = usePageHeader({ actions: headerActions });

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{headerActions}</div>
			) : null}
			{state.conflict ? (
				<Alert
					type="warning"
					showIcon
					title={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{pickedFor ? (
				<Panel title={t('footprints.pickerTitle')}>
					{mapState === 'failed' ? (
						<Alert
							type="warning"
							showIcon
							title={t('footprints.mapUnavailable')}
							description={t('footprints.mapUnavailableBody')}
						/>
					) : (
						<div ref={mapHostRef} style={{ height: 280 }} />
					)}
				</Panel>
			) : null}
			{(state.draft ?? state.baseline) === null || state.loading ? (
				<PageLoading spec="page" label={t('common.loading')} />
			) : (state.draft ?? state.baseline)!.cities.length === 0 ? (
				<p>{t('footprints.empty')}</p>
			) : (
				<Collapse
					items={(state.draft ?? state.baseline)!.cities.map((city, ci) => ({
						key: ci,
						label: `${city.name?.zh || city.id} · ${city.visitDate} · ${(city.places ?? []).length}`,
						children: (
							<div>
								<Space wrap style={{ marginBottom: 8 }}>
									<Input
										size="small"
										style={{ width: 140 }}
										placeholder="ID"
										value={city.id}
										disabled={!editable}
										onChange={(e) => updateCity(ci, { id: e.target.value })}
									/>
									<Input
										size="small"
										style={{ width: 110 }}
										placeholder="名称 zh"
										value={city.name?.zh}
										disabled={!editable}
										onChange={(e) => updateCity(ci, { name: { ...city.name, zh: e.target.value } })}
									/>
									<Input
										size="small"
										style={{ width: 110 }}
										placeholder="Name en"
										value={city.name?.en}
										disabled={!editable}
										onChange={(e) => updateCity(ci, { name: { ...city.name, en: e.target.value } })}
									/>
									<Input
										size="small"
										style={{ width: 110 }}
										placeholder="YYYYMMDD"
										value={city.visitDate}
										disabled={!editable}
										onChange={(e) => updateCity(ci, { visitDate: e.target.value })}
									/>
									{coordInputs(
										city.coordinates,
										(coord) => updateCity(ci, { coordinates: coord }),
										{ kind: 'city', cityIndex: ci }
									)}
									{editable ? (
										<Popconfirm
											title={t('footprints.removeCityConfirm')}
											onConfirm={() => mutate((cities) => cities.filter((_, i) => i !== ci))}
										>
											<Button size="small" danger>
												{t('posts.delete')}
											</Button>
										</Popconfirm>
									) : null}
								</Space>
								{(city.places ?? []).map((place, pi) => (
									<Card key={pi} size="small" style={{ marginBottom: 8 }}>
										<Space wrap>
											<Input
												size="small"
												style={{ width: 120 }}
												placeholder="ID"
												value={place.id}
												disabled={!editable}
												onChange={(e) => updatePlace(ci, pi, { id: e.target.value })}
											/>
											<Input
												size="small"
												style={{ width: 100 }}
												placeholder="名称 zh"
												value={place.name?.zh}
												disabled={!editable}
												onChange={(e) =>
													updatePlace(ci, pi, { name: { ...place.name, zh: e.target.value } })
												}
											/>
											<Input
												size="small"
												style={{ width: 100 }}
												placeholder="Name en"
												value={place.name?.en}
												disabled={!editable}
												onChange={(e) =>
													updatePlace(ci, pi, { name: { ...place.name, en: e.target.value } })
												}
											/>
											<Input
												size="small"
												style={{ width: 100 }}
												placeholder="YYYYMMDD"
												value={place.visitDate}
												disabled={!editable}
												onChange={(e) => updatePlace(ci, pi, { visitDate: e.target.value })}
											/>
											{coordInputs(
												place.coordinates,
												(coord) => updatePlace(ci, pi, { coordinates: coord }),
												{ kind: 'place', cityIndex: ci, placeIndex: pi }
											)}
											{editable ? (
												<Popconfirm
													title={t('footprints.removePlaceConfirm')}
													onConfirm={() =>
														mutate((cities) =>
															cities.map((c, i2) =>
																i2 === ci
																	? { ...c, places: (c.places ?? []).filter((_, i3) => i3 !== pi) }
																	: c
															)
														)
													}
												>
													<Button size="small" danger>
														×
													</Button>
												</Popconfirm>
											) : null}
										</Space>
									</Card>
								))}
								{editable ? (
									<Button size="small" onClick={() => addPlace(ci)}>
										{t('footprints.addPlace')}
									</Button>
								) : null}
							</div>
						)
					}))}
				/>
			)}
		</div>
	);
}
