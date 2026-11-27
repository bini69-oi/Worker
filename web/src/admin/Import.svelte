<script lang="ts">
  import type { AdminImportReport, AdminSettingResult } from '../lib/types.gen'
  import { api, errText } from './api'
  import { importView } from './importView'

  let view = $state<ReturnType<typeof importView> | null>(null)
  let error = $state('')
  let note = $state('')
  let busy = $state(false)

  async function preview() {
    error = ''
    note = ''
    busy = true
    try {
      view = importView(await api<AdminImportReport>('POST', '/import/preview'))
    } catch (e) {
      error = errText(e)
    } finally {
      busy = false
    }
  }

  async function apply() {
    error = ''
    try {
      await api<AdminSettingResult>('POST', '/import/apply')
      note = 'Подтвердите в Telegram: абоненты будут заведены после «Да», итог попадёт в журнал'
    } catch (e) {
      error = errText(e)
    }
  }
</script>

<section class="box">
  <h2>Импорт из панели</h2>
  <p class="muted">Сначала пробный прогон с отчётом, запись после «Да» в Telegram. Панель только читается.</p>
  <div class="row"><button disabled={busy} onclick={preview}>Пробный прогон</button></div>
  {#if error}<p class="err">{error}</p>{/if}
  {#if view}
    <p>{view.summary}</p>
    {#if view.reasons.length}<p class="muted">Пропущено: {view.reasons.join('; ')}</p>{/if}
    {#if view.examples.length}<ul class="muted">{#each view.examples as ex (ex)}<li>{ex}</li>{/each}</ul>{/if}
    <div class="row"><button class="primary" disabled={!view.canApply} onclick={apply}>Записать после «Да»</button></div>
  {/if}
  {#if note}<p class="ok">{note}</p>{/if}
</section>
