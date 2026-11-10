<!--
	Field: labeled form control supporting input, textarea, and select. The
	value is bindable; disabled state renders the view-only look.
-->
<script lang="ts">
	let {
		label,
		as = 'input',
		value = $bindable(''),
		type = 'text',
		disabled = false,
		hint = '',
		options = []
	}: {
		label: string;
		as?: 'input' | 'textarea' | 'select';
		value?: string;
		type?: string;
		disabled?: boolean;
		hint?: string;
		options?: Array<{ value: string; label: string }>;
	} = $props();
</script>

<label class="field">
	<span>{label}</span>
	{#if as === 'textarea'}
		<textarea {disabled} bind:value></textarea>
	{:else if as === 'select'}
		<select {disabled} bind:value>
			{#each options as opt (opt.value)}
				<option value={opt.value}>{opt.label}</option>
			{/each}
		</select>
	{:else}
		<input {type} {disabled} bind:value />
	{/if}
	{#if hint}
		<span class="hint">{hint}</span>
	{/if}
</label>
