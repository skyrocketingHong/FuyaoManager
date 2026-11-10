<script lang="ts">
	import { page } from '$app/state';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import { i18n, t } from '$lib/i18n/index.svelte';
</script>

<div class="card">
	<h1>{page.status}</h1>
	<p>
		{#if page.status === 404}
			<Crossfade inline key={i18n.locale}>{t('error.notFound')}</Crossfade>
		{:else}
			<Crossfade inline key={i18n.locale}>{t('error.generic')}</Crossfade>
		{/if}
	</p>
	<p><a href="/sites"><Crossfade inline key={i18n.locale}>{t('common.backHome')}</Crossfade></a></p>
</div>
