<!--
	StatusMessage: accessible inline status. Errors use role="alert" so screen
	readers announce them; success/info use role="status".
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		variant,
		children
	}: { variant: 'error' | 'ok' | 'warn' | 'info'; children: Snippet } = $props();
</script>

<div class="status" data-variant={variant} role={variant === 'error' ? 'alert' : 'status'}>
	{@render children()}
</div>
