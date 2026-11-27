<script lang="ts">
  import type { AdminMaintenance } from '../lib/types.gen'
  import { api, errText } from './api'
  import { maintenanceView } from './maintenanceView'

  let st = $state<AdminMaintenance | null>(null)
  let text = $state('')
  let error = $state('')

  const view = $derived(st ? maintenanceView(st) : null)

  async function load() {
    try {
      st = await api<AdminMaintenance>('GET', '/maintenance')
      text = st.text
    } catch (e) {
      error = errText(e)
    }
  }

  async function press() {
    if (!view) return
    error = ''
    try {
      st = await api<AdminMaintenance>('POST', '/maintenance', { on: view.next, text })
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

{#if view}
  <section class="box">
    <h2>Техработы</h2>
    <p>{view.status}</p>
    <label for="mt">Текст для абонентов</label>
    <textarea id="mt" bind:value={text}></textarea>
    <div class="row"><button class={view.next ? 'danger' : 'primary'} onclick={press}>{view.button}</button></div>
    {#if error}<p class="err">{error}</p>{/if}
  </section>
{/if}
