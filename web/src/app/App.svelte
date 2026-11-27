<script lang="ts">
  // Каркас кабинета: вход, блокировка, техработы и экраны по маршруту.
  import { useT } from '../lib/context'
  import { useApp } from './context'
  import Login from './Login.svelte'
  import Notice from './Notice.svelte'
  import Routes from './Routes.svelte'

  const t = useT()
  const { session, ep } = useApp()
  const state = session.state
  const failure = session.failure
  const revision = session.revision
</script>

{#if $state === 'booting'}
  <Notice title={$t('app.loading')} />
{:else if $state === 'login'}
  <Login {ep} onDone={() => session.loggedIn()} />
{:else if $state === 'banned'}
  <Notice title={$t('err.banned')} />
{:else if $state === 'failed'}
  <Notice title={$failure.status === 'offline' ? $t('app.offline') : $t('err.internal')} hint={$failure.status === 'offline' ? $t('app.offline_hint') : ''}>
    <button class="btn" type="button" onclick={() => session.boot()}>{$t('app.retry')}</button>
  </Notice>
{:else}
  {#key $revision}
    <Routes />
  {/key}
{/if}
