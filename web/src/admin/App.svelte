<script lang="ts">
  import { onMount } from 'svelte'
  import { session, loadMe, logout } from './session.svelte'
  import { visible } from './menu'
  import Login from './Login.svelte'
  import { screens } from './screens'

  let hash = $state(location.hash)
  let fail = $state('')

  onMount(() => {
    loadMe().catch(() => (fail = 'Не получилось связаться с сервером'))
    const onHash = () => (hash = location.hash)
    addEventListener('hashchange', onHash)
    return () => removeEventListener('hashchange', onHash)
  })

  const parts = $derived(hash.replace(/^#\/?/, '').split('/').filter(Boolean))
  const menu = $derived(session.me ? visible(session.me) : [])
  const current = $derived(parts[0] ?? menu[0]?.id ?? '')
  const Screen = $derived(menu.some((s) => s.id === current) ? screens[current] : undefined)
</script>

{#if fail}
  <main><p class="err">{fail}</p></main>
{:else if !session.loaded}
  <main><p class="muted">Загрузка…</p></main>
{:else if !session.me}
  <Login />
{:else}
  <nav class="menu">
    {#each menu as s (s.id)}
      <a href={'#/' + s.id} class:on={current === s.id}>{s.title}</a>
    {/each}
    <span class="grow"></span>
    <button onclick={() => logout()}>Выйти</button>
  </nav>
  <main>
    {#if Screen}
      <Screen arg={parts[1] ?? ''} />
    {:else}
      <p class="muted">Выберите раздел.</p>
    {/if}
  </main>
{/if}
