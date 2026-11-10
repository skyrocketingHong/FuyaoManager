<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { i18n, initLocale, setLocale, t, type Locale } from '$lib/i18n/index.svelte';
	import { initTheme, toggleTheme, theme } from '$lib/theme/index.svelte';
	import { session, loadSession, signOut } from '$lib/stores/session.svelte';

	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();
	let booted = $state(false);

	onMount(async () => {
		initLocale();
		initTheme();
		await loadSession();
		booted = true;
	});

	function switchLocale() {
		setLocale(i18n.locale === 'zh-CN' ? 'en-US' : 'zh-CN');
	}

	async function handleSignOut() {
		await signOut();
		await goto('/login');
	}

	function otherLocale(): Locale {
		return i18n.locale === 'zh-CN' ? 'en-US' : 'zh-CN';
	}
</script>

<div class="shell">
	<header class="topbar">
		<span class="brand"><Crossfade inline key={i18n.locale}>{t('app.title')}</Crossfade></span>
		<button class="link-like" onclick={switchLocale} aria-label={otherLocale()}>
			<Crossfade inline key={i18n.locale}>{t('app.localeSwitch')}</Crossfade>
		</button>
		<button class="link-like" onclick={toggleTheme}>
			<Crossfade inline key={`${i18n.locale}:${theme.dark}`}>
				{theme.dark ? t('app.theme.light') : t('app.theme.dark')}
			</Crossfade>
		</button>
		{#if booted && session.status === 'authenticated' && session.user}
			<span class="badge">{session.user.username}</span>
			<Button variant="link-like" onclick={handleSignOut}>
				<Crossfade inline key={i18n.locale}>{t('app.signOut')}</Crossfade>
			</Button>
		{/if}
	</header>

	<main>
		{#if !booted}
			<p role="status"><Crossfade inline key={i18n.locale}>{t('common.loading')}</Crossfade></p>
		{:else}
			{@render children()}
		{/if}
	</main>
</div>
