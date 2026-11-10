<!--
	Crossfade: fades between locale-specific content on key change. Renders a
	span in inline mode (required inside p/h1-h6/span/button/a) and a div
	otherwise, matching the project hydration rules.
-->
<script lang="ts">
	import { fade } from 'svelte/transition';
	import type { Snippet } from 'svelte';

	let {
		key,
		inline = false,
		children
	}: { key: string | number; inline?: boolean; children: Snippet } = $props();
</script>

{#key key}
	{#if inline}
		<span class="crossfade" in:fade={{ duration: 160 }} out:fade={{ duration: 160 }}
			>{@render children()}</span
		>
	{:else}
		<div class="crossfade" in:fade={{ duration: 160 }} out:fade={{ duration: 160 }}
			>{@render children()}</div
		>
	{/if}
{/key}
