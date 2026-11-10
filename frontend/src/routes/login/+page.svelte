<script lang="ts">
	import { goto } from '$app/navigation';
	import Button from '$lib/components/ui/Button.svelte';
	import StatusMessage from '$lib/components/ui/StatusMessage.svelte';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import { loginWithPasskey } from '$lib/api/passkey';
	import { i18n, t } from '$lib/i18n/index.svelte';
	import { apiErrorKey } from '$lib/i18n/errors';
	import { markSignedIn, session } from '$lib/stores/session.svelte';

	let busy = $state(false);
	let errorKey = $state<ReturnType<typeof apiErrorKey>>(null);

	async function handleLogin() {
		busy = true;
		errorKey = null;
		try {
			const done = await loginWithPasskey();
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
	<h2><Crossfade inline key={i18n.locale}>{t('login.title')}</Crossfade></h2>
	<p>
		<Crossfade inline key={i18n.locale}>{t('login.description')}</Crossfade>
	</p>
	{#if errorKey}
		<StatusMessage variant="error"><Crossfade key={errorKey}>{t(errorKey)}</Crossfade></StatusMessage>
	{/if}
	<Button variant="primary" loading={busy} onclick={handleLogin}>
		<Crossfade inline key={i18n.locale}>{t('login.action')}</Crossfade>
	</Button>
	<p class="hint" style="margin-top:14px">
		<a href="/bootstrap"><Crossfade inline key={i18n.locale}>{t('login.firstTime')}</Crossfade></a>
	</p>
</div>
