<script lang="ts">
	import { goto } from '$app/navigation';
	import Button from '$lib/components/ui/Button.svelte';
	import StatusMessage from '$lib/components/ui/StatusMessage.svelte';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import { enrollPasskey } from '$lib/api/passkey';
	import { i18n, t } from '$lib/i18n/index.svelte';
	import { apiErrorKey } from '$lib/i18n/errors';
	import { markSignedIn } from '$lib/stores/session.svelte';

	let permit = $state('');
	let busy = $state(false);
	let errorKey = $state<ReturnType<typeof apiErrorKey>>(null);

	async function handleEnroll() {
		if (permit.trim() === '') return;
		busy = true;
		errorKey = null;
		try {
			const done = await enrollPasskey(permit.trim());
			markSignedIn(done.user);
			await goto('/sites');
		} catch (err) {
			errorKey = apiErrorKey(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="card">
	<h2><Crossfade inline key={i18n.locale}>{t('bootstrap.title')}</Crossfade></h2>
	<p>
		<Crossfade inline key={i18n.locale}>{t('bootstrap.description')}</Crossfade>
	</p>
	<form onsubmit={(e) => { e.preventDefault(); handleEnroll(); }}>
		<label class="field">
			<span>{t('bootstrap.permitLabel')}</span>
			<input class="permit" bind:value={permit} autocomplete="off" spellcheck="false" />
			<span class="hint">{t('bootstrap.permitHint')}</span>
		</label>
		{#if errorKey}
			<StatusMessage variant="error"><Crossfade key={errorKey}>{t(errorKey)}</Crossfade></StatusMessage>
		{/if}
		<Button variant="primary" type="submit" loading={busy}>
			<Crossfade inline key={i18n.locale}>{t('bootstrap.action')}</Crossfade>
		</Button>
	</form>
</div>
