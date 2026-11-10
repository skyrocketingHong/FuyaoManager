<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import { listSites, type Site } from '$lib/api/management';
	import { i18n, t } from '$lib/i18n/index.svelte';
	import { apiErrorKey } from '$lib/i18n/errors';
	import { session } from '$lib/stores/session.svelte';

	let sites = $state<Site[] | null>(null);
	let loadError = $state(false);

	onMount(async () => {
		if (session.status === 'anonymous') {
			await goto('/login');
			return;
		}
		try {
			sites = await listSites();
		} catch (err) {
			if (apiErrorKey(err) === 'error.unauthenticated') {
				await goto('/login');
				return;
			}
			loadError = true;
		}
	});

	function roleKey(role: Site['role']) {
		return `role.${role}` as const;
	}
</script>

<h1><Crossfade inline key={i18n.locale}>{t('sites.title')}</Crossfade></h1>

{#if sites === null}
	<p role="status"><Crossfade inline key={i18n.locale}>{t('common.loading')}</Crossfade></p>
{:else if loadError}
	<p role="alert"><Crossfade inline key={i18n.locale}>{t('error.generic')}</Crossfade></p>
{:else if sites.length === 0}
	<div class="card"><Crossfade inline key={i18n.locale}>{t('sites.empty')}</Crossfade></div>
{:else}
	<div class="card-list">
		{#each sites as site (site.siteId)}
			<a class="card" href="/sites/{site.siteId}">
				<h2>{site.name}</h2>
				<div class="meta">
					<dt>{t('site.slug')}</dt>
					<dd>{site.slug}</dd>
					<dt>{t('site.role')}</dt>
					<dd><span class="badge"><Crossfade inline key={i18n.locale}>{t(roleKey(site.role))}</Crossfade></span></dd>
					<dt>{t('site.locale')}</dt>
					<dd>{site.defaultLocale}</dd>
				</div>
			</a>
		{/each}
	</div>
{/if}
