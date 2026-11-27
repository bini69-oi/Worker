<script lang="ts">
  import type { AdminPromoList } from '../lib/types.gen'
  import { api, errText } from './api'
  import { parseRub } from './format'
  import { promoView } from './catalogView'

  let rows = $state<ReturnType<typeof promoView>[]>([])
  let next = $state(0)
  let error = $state('')
  let note = $state('')
  let code = $state('')
  let kind = $state('days')
  let value = $state('7')
  let maxUses = $state('0')

  async function load(cursor: number) {
    try {
      const res = await api<AdminPromoList>('GET', '/promos' + (cursor ? `?cursor=${cursor}` : ''))
      const view = res.items.map(promoView)
      rows = cursor ? [...rows, ...view] : view
      next = res.next
    } catch (e) {
      error = errText(e)
    }
  }

  async function create(e: Event) {
    e.preventDefault()
    error = ''
    note = ''
    const v = kind === 'days' ? Number(value) : parseRub(value)
    const uses = Number(maxUses)
    if (v === null || !Number.isInteger(v) || v <= 0 || !Number.isInteger(uses) || uses < 0) {
      error = 'Значение: дни целым числом или рубли, лимит: целое число'
      return
    }
    try {
      await api('POST', '/promos', { code, kind, value: v, max_uses: uses })
      note = 'Промокод создан'
      code = ''
      await load(0)
    } catch (err) {
      error = errText(err)
    }
  }

  async function toggle(id: number, active: boolean) {
    try {
      await api('POST', `/promos/${id}/active`, { active })
      await load(0)
    } catch (e) {
      error = errText(e)
    }
  }

  async function copy(link: string) {
    try {
      await navigator.clipboard.writeText(link)
      note = 'Ссылка скопирована'
    } catch {
      error = 'Браузер не дал скопировать: выделите ссылку вручную'
    }
  }

  $effect(() => {
    void load(0)
  })
</script>

<h1>Промокоды</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}
<section class="box">
  <form class="row" onsubmit={create}>
    <input placeholder="Код, 6–32 знака" aria-label="Код" bind:value={code} />
    <select bind:value={kind} aria-label="Вид"><option value="days">дни</option><option value="money">деньги, ₽</option></select>
    <input size="6" aria-label="Сколько" bind:value={value} />
    <label for="mu">лимит активаций</label><input id="mu" size="4" bind:value={maxUses} />
    <button class="primary">Создать</button>
  </form>
</section>
<table class="tbl">
  <thead><tr><th>Код</th><th>Даёт</th><th>Активаций</th><th>Действует до</th><th>Ссылка</th><th></th></tr></thead>
  <tbody>
    {#each rows as r (r.id)}
      <tr>
        <td data-label="Код">{r.code}{r.active ? '' : ' (выключен)'}</td>
        <td data-label="Даёт">{r.gives}</td>
        <td data-label="Активаций">{r.uses}</td>
        <td data-label="Действует до">{r.until}</td>
        <td data-label="Ссылка">
          {#if r.link}<button onclick={() => copy(r.link)}>Скопировать</button>{:else}<span class="muted">нет имени бота</span>{/if}
        </td>
        <td><button onclick={() => toggle(r.id, !r.active)}>{r.active ? 'Выключить' : 'Включить'}</button></td>
      </tr>
    {/each}
  </tbody>
</table>
{#if next}<p><button onclick={() => load(next)}>Ещё</button></p>{/if}
