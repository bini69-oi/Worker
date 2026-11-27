<script lang="ts">
  import type { AdminGatewayList, AdminSettingResult } from '../lib/types.gen'
  import { api, errText } from './api'
  import { gatewayView, settingNote } from './configView'
  import SettingRow from './SettingRow.svelte'

  let rows = $state<ReturnType<typeof gatewayView>[]>([])
  let error = $state('')
  let note = $state('')

  async function load() {
    try {
      rows = (await api<AdminGatewayList>('GET', '/gateways')).items.map(gatewayView)
    } catch (e) {
      error = errText(e)
    }
  }

  async function toggle(name: string, on: boolean) {
    error = ''
    note = ''
    try {
      const res = await api<AdminSettingResult>('POST', '/settings/set', { key: `${name}.enabled`, value: String(on) })
      note = settingNote(res)
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Кассы</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}
{#each rows as g (g.name)}
  <section class="box">
    <h2>{g.title}</h2>
    <dl class="kv">
      <dt>Состояние</dt><dd>{g.state}</dd>
      <dt>Ключи</dt><dd class={g.canEnable ? '' : 'err'}>{g.keys}</dd>
      <dt>Последний оплаченный</dt><dd>{g.lastPaid}</dd>
      <dt>Ждут оплаты</dt><dd>{g.pending}</dd>
    </dl>
    <div class="row">
      {#if g.enabled}
        <button onclick={() => toggle(g.name, false)}>Выключить</button>
      {:else}
        <button class="primary" disabled={!g.canEnable} onclick={() => toggle(g.name, true)}>Включить</button>
        {#if !g.canEnable}<span class="muted">Сначала заполните ключи</span>{/if}
      {/if}
    </div>
    <details>
      <summary>Настройки кассы и ключи</summary>
      {#each g.settings as s (s.key)}<SettingRow {s} onchange={load} />{/each}
    </details>
  </section>
{/each}
