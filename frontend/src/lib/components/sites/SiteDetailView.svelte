<!--
	SiteDetailView: page composition for one site's detail. Owned by the
	route page, which keys this component on siteId so every navigation
	between sites re-initializes the whole subtree (no cross-site state).
	Business logic lives in lib/sites/site-detail.svelte.ts.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import StatusMessage from '$lib/components/ui/StatusMessage.svelte';
	import { createSiteDetail } from '$lib/sites/site-detail.svelte';
	import { i18n, t } from '$lib/i18n/index.svelte';

	let { siteId }: { siteId: string } = $props();

	const detail = createSiteDetail();

	onMount(() => {
		void detail.load(siteId, async () => {
			await goto('/login');
		});
	});

	function roleKey(role: 'owner' | 'editor' | 'viewer') {
		return `role.${role}` as const;
	}
</script>

{#if detail.state.site}
	<h1>{detail.state.site.name}</h1>
	<div class="card">
		<div class="meta">
			<dt>{t('site.slug')}</dt>
			<dd>{detail.state.site.slug}</dd>
			<dt>{t('site.role')}</dt>
			<dd>
				<span class="badge">
					<Crossfade inline key={i18n.locale}>{t(roleKey(detail.state.site.role))}</Crossfade>
				</span>
			</dd>
			<dt>{t('site.locale')}</dt>
			<dd>{detail.state.site.defaultLocale}</dd>
			<dt>{t('site.revision')}</dt>
			<dd>{detail.state.site.settingsRevision}</dd>
			<dt>{t('site.updatedAt')}</dt>
			<dd>{new Date(detail.state.site.updatedAt).toLocaleString(i18n.locale)}</dd>
			<dt>{t('site.createdAt')}</dt>
			<dd>{new Date(detail.state.site.createdAt).toLocaleString(i18n.locale)}</dd>
		</div>
		<p style="margin-top:16px">
			<a href="/sites/{detail.state.site.siteId}/settings">
				<Crossfade inline key={i18n.locale}>{t('site.openSettings')}</Crossfade>
			</a>
		</p>
	</div>
{:else if detail.state.errorKey}
	<div class="card">
		<StatusMessage variant="error">
			<Crossfade inline key={detail.state.errorKey}>{t(detail.state.errorKey as never)}</Crossfade>
		</StatusMessage>
	</div>
{:else}
	<p role="status"><Crossfade inline key={i18n.locale}>{t('common.loading')}</Crossfade></p>
{/if}
