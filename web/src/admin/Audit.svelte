<script lang="ts">
  import type { AdminAuditList } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { actionTitles, auditView } from './teamView'

  let admin = $state('')
  let action = $state('')
  let from = $state('')
  let to = $state('')
  let rows = $state<ReturnType<typeof auditView>[]>([])
  let next = $state(0)
  let error = $state('')

  async function load(cursor: number) {
    error = ''
    try {
      const res = await api<AdminAuditList>('GET', '/audit' + qs({ admin, action, from, to, cursor }))
      const view = res.items.map(auditView)
      rows = cursor ? [...rows, ...view] : view
      next = res.next
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load(0)
  })
</script>

<h1>Журнал действий</h1>
<form class="row" onsubmit={(e) => { e.preventDefault(); void load(0) }}>
  <input size="12" placeholder="Telegram ID админа" aria-label="Админ" inputmode="numeric" bind:value={admin} />
  <select aria-label="Действие" bind:value={action}>
    <option value="">любое действие</option>
    {#each Object.entries(actionTitles) as [id, title] (id)}<option value={id}>{title}</option>{/each}
  </select>
  <label for="af">с</label><input id="af" type="date" bind:value={from} />
  <label for="at">по</label><input id="at" type="date" bind:value={to} />
  <button class="primary">Показать</button>
</form>
{#if error}<p class="err">{error}</p>{/if}
<table class="tbl">
  <thead><tr><th>Когда</th><th>Админ</th><th>Действие</th><th>Над чем</th><th>Подробности</th><th>IP</th></tr></thead>
  <tbody>
    {#each rows as r (r.id)}
      <tr>
        <td data-label="Когда">{r.when}</td><td data-label="Админ">{r.admin}</td><td data-label="Действие">{r.action}</td>
        <td data-label="Над чем">{r.target}</td><td data-label="Подробности">{r.details}</td><td data-label="IP">{r.ip}</td>
      </tr>
    {/each}
  </tbody>
</table>
{#if next}<p><button onclick={() => load(next)}>Ещё</button></p>{/if}
