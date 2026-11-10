<script lang="ts">
	import { page } from '$app/state';
	import SiteDetailView from '$lib/components/sites/SiteDetailView.svelte';

	// Keying the composition on siteId fully re-creates it (and its business
	// module state) when the route param changes, so no data, ETag, or edit
	// state can survive a switch from one site to another.
</script>

{#if page.params.siteId}
	{#key page.params.siteId}
		<SiteDetailView siteId={page.params.siteId} />
	{/key}
{/if}
