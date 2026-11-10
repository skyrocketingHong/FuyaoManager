<script lang="ts">
	import { goto } from '$app/navigation';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import { i18n, t } from '$lib/i18n/index.svelte';
	import { session } from '$lib/stores/session.svelte';

	// Entry router: authenticated users go to their sites, others to login.
	$effect(() => {
		if (session.status === 'authenticated') goto('/sites', { replaceState: true });
		if (session.status === 'anonymous') goto('/login', { replaceState: true });
	});
</script>

<p role="status"><Crossfade inline key={i18n.locale}>{t('common.loading')}</Crossfade></p>
