<script lang="ts">
	import { page } from '$app/state';
	import SiteSettingsView from '$lib/components/sites/SiteSettingsView.svelte';

	// Keying the composition on siteId fully re-creates it (and its business
	// module state) when the route param changes, so no data, ETag, or edit
	// state can survive a switch from one site to another.
</script>

{#if page.params.siteId}
	{#key page.params.siteId}
		<SiteSettingsView siteId={page.params.siteId} />
	{/key}
{/if}
