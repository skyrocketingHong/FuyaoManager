<!--
	Button: thin styled wrapper (global app.css provides button and primary
	variants). Loading disables interaction without replacing the label.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		variant = 'default',
		type = 'button',
		disabled = false,
		loading = false,
		onclick,
		children
	}: {
		variant?: 'default' | 'primary' | 'link-like';
		type?: 'button' | 'submit';
		disabled?: boolean;
		loading?: boolean;
		onclick?: (e: MouseEvent) => void;
		children: Snippet;
	} = $props();
</script>

<button {type} class={variant} disabled={disabled || loading} {onclick}>{@render children()}</button>
