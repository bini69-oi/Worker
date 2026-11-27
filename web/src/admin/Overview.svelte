<script lang="ts">
  import type { AdminStats } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { overviewView, range } from './overviewView'

  let from = $state(range(1).from)
  let to = $state(range(1).to)
  let view = $state<ReturnType<typeof overviewView> | null>(null)
  let error = $state('')

  async function load() {
    error = ''
    try {
      view = overviewView(await api<AdminStats>('GET', '/stats' + qs({ from, to })))
    } catch (e) {
      error = errText(e)
    }
  }

  function quick(days: number) {
    ;({ from, to } = range(days))
    void load()
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Сводка</h1>
<div class="row">
  <button onclick={() => quick(1)}>Сегодня</button>
  <button onclick={() => quick(7)}>7 дней</button>
  <button onclick={() => quick(30)}>30 дней</button>
  <label for="of">с</label><input id="of" type="date" bind:value={from} />
  <label for="ot">по</label><input id="ot" type="date" bind:value={to} />
  <button class="primary" onclick={load}>Показать</button>
</div>
{#if error}<p class="err">{error}</p>{/if}
{#if view}
  {#if view.stuck}
    <section class="box">
      <strong class="err">Зависшие задачи панели: {view.stuck.count}</strong>
      <p class="muted">Панель не отвечает дольше часа. Абоненты:
        {#each view.stuck.users as u (u)}<a href={'#/users/' + u}> {u}</a>{/each}
      </p>
    </section>
  {/if}
  <div class="row">
    {#each view.tiles as t (t.title)}
      <section class="box"><div class="muted">{t.title}</div><strong>{t.value}</strong></section>
    {/each}
  </div>
  <h2>По дням</h2>
  <table class="tbl">
    <thead><tr><th>День</th><th>Выручка</th><th></th></tr></thead>
    <tbody>
      {#each view.days as d (d.day)}
        <tr>
          <td data-label="День">{d.day}</td>
          <td data-label="Выручка" class={d.negative ? 'err' : ''}>{d.revenue} <span class="muted">{d.refunded}</span></td>
          <td data-label="Доля"><progress max="100" value={d.share}></progress></td>
        </tr>
      {/each}
    </tbody>
  </table>
  <h2>По кассам</h2>
  <table class="tbl">
    <thead><tr><th>Касса</th><th>Выручка</th></tr></thead>
    <tbody>
      {#each view.gateways as g (g.name)}<tr><td data-label="Касса">{g.name}</td><td data-label="Выручка">{g.revenue}</td></tr>{/each}
    </tbody>
  </table>
{/if}
