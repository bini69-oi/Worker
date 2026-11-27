<script lang="ts">
  import type { AdminUserList } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { filters, rowView } from './usersView'
  import UserCard from './UserCard.svelte'

  let { id = 0 }: { id?: number } = $props()

  let q = $state('')
  let filter = $state('')
  let rows = $state<ReturnType<typeof rowView>[]>([])
  let next = $state(0)
  let error = $state('')
  let loaded = $state(false)

  async function load(cursor: number) {
    error = ''
    try {
      const res = await api<AdminUserList>('GET', '/users' + qs({ q, filter, cursor }))
      rows = cursor ? [...rows, ...res.items.map(rowView)] : res.items.map(rowView)
      next = res.next
      loaded = true
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    if (!id && !loaded) void load(0)
  })
</script>

{#if id}
  <p><a href="#/users">← К списку</a></p>
  <UserCard {id} />
{:else}
  <h1>Абоненты</h1>
  <form
    class="row"
    onsubmit={(e) => {
      e.preventDefault()
      void load(0)
    }}
  >
    <input placeholder="Telegram ID, @имя или код" bind:value={q} aria-label="Поиск" />
    <select bind:value={filter} aria-label="Фильтр" onchange={() => load(0)}>
      {#each filters as f (f.id)}<option value={f.id}>{f.title}</option>{/each}
    </select>
    <button class="primary">Найти</button>
  </form>
  {#if error}<p class="err">{error}</p>{/if}
  <table class="tbl">
    <thead><tr><th>Абонент</th><th>Telegram ID</th><th>Статус</th><th>До</th><th>Баланс</th></tr></thead>
    <tbody>
      {#each rows as r (r.id)}
        <tr>
          <td data-label="Абонент"><a href={'#/users/' + r.id}>{r.name}</a></td>
          <td data-label="Telegram ID">{r.tg}</td>
          <td data-label="Статус">{r.status}</td>
          <td data-label="До">{r.until}</td>
          <td data-label="Баланс">{r.balance}</td>
        </tr>
      {/each}
    </tbody>
  </table>
  {#if loaded && rows.length === 0}<p class="muted">Никого не нашли.</p>{/if}
  {#if next}<p><button onclick={() => load(next)}>Ещё</button></p>{/if}
{/if}
