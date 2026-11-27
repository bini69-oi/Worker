<script lang="ts">
  import type { AdminSquad, AdminTariff, AdminTariffList } from '../lib/types.gen'
  import { api, errText } from './api'
  import { rub } from './format'
  import { emptyTariff, formToTariff, resets, squadChoices, tariffToForm, type TariffForm } from './catalogView'

  let tariffs = $state<AdminTariff[]>([])
  let squads = $state<AdminSquad[]>([])
  let squadsOk = $state(true)
  let form = $state<TariffForm | null>(null)
  let error = $state('')
  let note = $state('')

  const choices = $derived(form ? squadChoices(squads, form.squads) : [])

  async function load() {
    try {
      const res = await api<AdminTariffList>('GET', '/tariffs')
      tariffs = res.tariffs
      squads = res.squads
      squadsOk = res.squads_ok
    } catch (e) {
      error = errText(e)
    }
  }

  async function save() {
    if (!form) return
    error = ''
    const r = formToTariff(form)
    if ('error' in r) {
      error = r.error
      return
    }
    try {
      await api('POST', form.id ? `/tariffs/${form.id}` : '/tariffs', r.ok)
      note = 'Сохранено'
      form = null
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  function toggle(uuid: string, on: boolean) {
    if (!form) return
    form.squads = on ? [...form.squads, uuid] : form.squads.filter((u) => u !== uuid)
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Тарифы</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}

{#if form}
  <section class="box">
    <div class="row"><label for="tn">Название</label><input id="tn" bind:value={form.name} /></div>
    <div class="row">
      <label for="tt">Трафик, ГБ</label><input id="tt" size="6" bind:value={form.trafficGb} />
      <label for="td">Устройств</label><input id="td" size="4" bind:value={form.devices} />
      <span class="muted">0 без лимита</span>
    </div>
    <div class="row">
      <label for="tr">Сброс трафика</label>
      <select id="tr" bind:value={form.reset}>{#each resets as r (r.id)}<option value={r.id}>{r.title}</option>{/each}</select>
      <label for="ts">Порядок</label><input id="ts" size="4" bind:value={form.sort} />
      <label><input type="checkbox" bind:checked={form.active} /> показывать</label>
    </div>
    <h2>Сроки и цены</h2>
    {#each form.periods as p, i (i)}
      <div class="row">
        <input size="5" aria-label="Дней" bind:value={p.days} /> дн.
        <input size="8" aria-label="Цена, ₽" bind:value={p.price} /> ₽
        <button onclick={() => form?.periods.splice(i, 1)}>Убрать</button>
      </div>
    {/each}
    <div class="row"><button onclick={() => form?.periods.push({ days: '', price: '' })}>Добавить срок</button></div>
    <h2>Сквады панели</h2>
    {#if !squadsOk}<p class="err">Панель не ответила: список сквадов недоступен</p>{/if}
    {#each choices as c (c.uuid)}
      <div><label><input type="checkbox" checked={c.on} onchange={(e) => toggle(c.uuid, e.currentTarget.checked)} /> {c.name}</label></div>
    {/each}
    <div class="row">
      <button class="primary" onclick={save}>Сохранить</button>
      <button onclick={() => (form = null)}>Отмена</button>
    </div>
  </section>
{:else}
  <p><button class="primary" onclick={() => (form = emptyTariff())}>Новый тариф</button></p>
  <table class="tbl">
    <thead><tr><th>Название</th><th>Сроки</th><th>Показывать</th><th></th></tr></thead>
    <tbody>
      {#each tariffs as t (t.id)}
        <tr>
          <td data-label="Название">{t.name}</td>
          <td data-label="Сроки">{t.periods.map((p) => `${p.days} дн. ${rub(p.price)}`).join(', ')}</td>
          <td data-label="Показывать">{t.active ? 'да' : 'скрыт'}</td>
          <td><button onclick={() => (form = tariffToForm(t))}>Править</button></td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}
