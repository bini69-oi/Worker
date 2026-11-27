<script lang="ts">
  import type { AdminSettingList } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { settingView } from './configView'
  import SettingRow from './SettingRow.svelte'

  let q = $state('')
  let group = $state('')
  let rows = $state<ReturnType<typeof settingView>[]>([])
  let groups = $state<string[]>([])
  let error = $state('')

  async function load() {
    error = ''
    try {
      const res = await api<AdminSettingList>('GET', '/settings' + qs({ q, group }))
      rows = res.items.map(settingView)
      if (!groups.length) {
        const all = await api<AdminSettingList>('GET', '/settings')
        groups = [...new Set(all.items.map((s) => s.group))].sort()
      }
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Настройки</h1>
<form class="row" onsubmit={(e) => { e.preventDefault(); void load() }}>
  <input placeholder="Поиск по ключу или описанию" aria-label="Поиск" bind:value={q} />
  <select aria-label="Группа" bind:value={group} onchange={load}>
    <option value="">все группы</option>
    {#each groups as g (g)}<option value={g}>{g}</option>{/each}
  </select>
  <button class="primary">Найти</button>
</form>
{#if error}<p class="err">{error}</p>{/if}
{#each rows as s (s.key)}<SettingRow {s} onchange={load} />{/each}
