<script lang="ts">
  import type { AdminPaymentList } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { gateways, payStatus, payView } from './paymentsView'
  import { session } from './session.svelte'

  let status = $state('')
  let gateway = $state('')
  let from = $state('')
  let to = $state('')
  let rows = $state<ReturnType<typeof payView>[]>([])
  let next = $state(0)
  let error = $state('')
  let note = $state('')
  let loaded = $state(false)

  async function load(cursor: number) {
    error = ''
    try {
      const res = await api<AdminPaymentList>('GET', '/payments' + qs({ status, gateway, from, to, cursor }))
      const view = res.items.map((p) => payView(p, session.me?.owner ?? false))
      rows = cursor ? [...rows, ...view] : view
      next = res.next
      loaded = true
    } catch (e) {
      error = errText(e)
    }
  }

  async function act(id: number, what: 'confirm' | 'refund') {
    error = ''
    note = ''
    try {
      await api('POST', `/payments/${id}/${what}`)
      note = what === 'confirm' ? 'Платёж зачислен' : 'Платёж возвращён'
    } catch (e) {
      error = errText(e)
    }
    await load(0)
  }

  $effect(() => {
    if (!loaded) void load(0)
  })
</script>

<h1>Платежи</h1>
<form
  class="row"
  onsubmit={(e) => {
    e.preventDefault()
    void load(0)
  }}
>
  <select bind:value={status} aria-label="Статус">
    <option value="">любой статус</option>
    {#each Object.entries(payStatus) as [id, title] (id)}<option value={id}>{title}</option>{/each}
  </select>
  <select bind:value={gateway} aria-label="Касса">
    {#each gateways as g (g.id)}<option value={g.id}>{g.title}</option>{/each}
  </select>
  <label for="from">с</label><input id="from" type="date" bind:value={from} />
  <label for="to">по</label><input id="to" type="date" bind:value={to} />
  <button class="primary">Показать</button>
</form>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}
<table class="tbl">
  <thead><tr><th>Абонент</th><th>Касса</th><th>Сумма</th><th>Статус</th><th>Создан</th><th>Оплачен</th><th></th></tr></thead>
  <tbody>
    {#each rows as r (r.id)}
      <tr>
        <td data-label="Абонент"><a href={'#/users/' + r.userId}>{r.user}</a></td>
        <td data-label="Касса">{r.gateway}</td>
        <td data-label="Сумма">{r.amount}</td>
        <td data-label="Статус">{r.status}</td>
        <td data-label="Создан">{r.created}</td>
        <td data-label="Оплачен">{r.paid}</td>
        <td>
          {#if r.canConfirm}<button class="primary" onclick={() => act(r.id, 'confirm')}>Подтвердить</button>{/if}
          {#if r.canRefund}<button class="danger" onclick={() => act(r.id, 'refund')}>Вернуть</button>{/if}
        </td>
      </tr>
    {/each}
  </tbody>
</table>
{#if loaded && rows.length === 0}<p class="muted">Платежей нет.</p>{/if}
{#if next}<p><button onclick={() => load(next)}>Ещё</button></p>{/if}
