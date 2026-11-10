<!--
	SiteSettingsView: page composition for one site's settings editor. The
	route page keys this component on siteId so every navigation between
	sites re-initializes the editor (no cross-site state, no stale ETag).
	Business logic lives in lib/sites/settings-editor.svelte.ts.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Crossfade from '$lib/components/ui/Crossfade.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Field from '$lib/components/ui/Field.svelte';
	import StatusMessage from '$lib/components/ui/StatusMessage.svelte';
	import { createSettingsEditor } from '$lib/sites/settings-editor.svelte';
	import { i18n, t } from '$lib/i18n/index.svelte';
	import { session } from '$lib/stores/session.svelte';

	let { siteId }: { siteId: string } = $props();

	const editor = createSettingsEditor({
		onUnauthenticated: async () => {
			await goto('/login');
		}
	});

	onMount(() => {
		if (session.status === 'anonymous') goto('/login');
		void editor.load(siteId);
	});

	const localeOptions = $derived([
		{ value: 'zh-CN', label: t('settings.locale.zh-CN') },
		{ value: 'en-US', label: t('settings.locale.en-US') }
	]);
	const editable = $derived(editor.editable());
	const phase = $derived(editor.state.phase);
	const saveLabel = $derived(phase.kind === 'saving' ? t('common.saving') : t('common.save'));
	const dirtyCount = $derived(editor.dirtyFields().length);
</script>

<h1>
	<Crossfade inline key={i18n.locale}>{t('settings.title')}</Crossfade>{#if editor.state.site}
		<span style="color:var(--text-muted);font-weight:400"> · {editor.state.site.name}</span
		>{/if}
</h1>

{#if phase.kind === 'error' && editor.state.site === null}
	<div class="card">
		<StatusMessage variant="error">
			<Crossfade inline key={phase.key}>{t(phase.key as never)}</Crossfade>
		</StatusMessage>
	</div>
{:else if editor.state.site}
	<div class="card">
		{#if !editable}
			<StatusMessage variant="warn">
				<Crossfade inline key={i18n.locale}>{t('settings.viewerNotice')}</Crossfade>
			</StatusMessage>
		{/if}

		<form onsubmit={(e) => { e.preventDefault(); editor.save(); }}>
			<Field label={t('settings.name')} disabled={!editable} bind:value={editor.state.name} />
			<Field
				label={t('settings.description')}
				as="textarea"
				disabled={!editable}
				bind:value={editor.state.description}
			/>
			<Field
				label={t('settings.defaultLocale')}
				as="select"
				disabled={!editable}
				bind:value={editor.state.defaultLocale}
				options={localeOptions}
			/>

			{#if phase.kind === 'saved' && dirtyCount === 0}
				<StatusMessage variant="ok">
					<Crossfade inline key={i18n.locale}>
						{t('settings.savedAt').replace('{revision}', String(phase.revision))}
					</Crossfade>
				</StatusMessage>
			{:else if phase.kind === 'saved' && dirtyCount > 0}
				<StatusMessage variant="ok">
					<Crossfade inline key={i18n.locale}>
						{t('settings.savedWithEdits').replace('{revision}', String(phase.revision))}
					</Crossfade>
				</StatusMessage>
			{:else if phase.kind === 'conflict'}
				<div class="status" data-variant="warn" role="alert">
					<p>
						<strong>
							<Crossfade inline key={i18n.locale}>{t('settings.conflictTitle')}</Crossfade>
						</strong>
					</p>
					<p>
						<Crossfade inline key={i18n.locale}>{t('settings.conflictBody')}</Crossfade>
					</p>
					{#if phase.synced}
						<p>
							<Crossfade inline key={i18n.locale}>{t('settings.conflictSynced')}</Crossfade>
						</p>
					{:else}
						<Button onclick={() => editor.syncAfterConflict()}>
							<Crossfade inline key={i18n.locale}>{t('settings.conflictAction')}</Crossfade>
						</Button>
					{/if}
				</div>
			{:else if phase.kind === 'error'}
				<StatusMessage variant="error">
					<Crossfade inline key={phase.key}>{t(phase.key as never)}</Crossfade>
				</StatusMessage>
			{/if}

			{#if editable}
				<Button variant="primary" type="submit" loading={phase.kind === 'saving'} disabled={dirtyCount === 0}>
					<Crossfade inline key={i18n.locale}>{saveLabel}</Crossfade>
				</Button>
			{/if}
		</form>
	</div>
{:else}
	<p role="status"><Crossfade inline key={i18n.locale}>{t('common.loading')}</Crossfade></p>
{/if}
