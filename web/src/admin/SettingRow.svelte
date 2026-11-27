<script lang="ts">
  import { api, errText } from './api'
  import { settingNote, settingView } from './configView'
  import type { AdminSettingResult } from '../lib/types.gen'

  let { s, onchange }: { s: ReturnType<typeof settingView>; onchange: () => void } = $props()

  let value = $state('')
  let note = $state('')
  let error = $state('')

  $effect(() => {
    value = s.input
  })

  async function send(path: 'set' | 'reset') {
    error = ''
    note = ''
    try {
      const res = await api<AdminSettingResult>('POST', `/settings/${path}`, path === 'set' ? { key: s.key, value } : { key: s.key })
      note = settingNote(res)
      if (!res.pending) onchange()
    } catch (e) {
      error = errText(e)
    }
  }
</script>

<section class="box">
  <strong>{s.key}</strong>
  {#if s.changed}<span class="ok"> изменено</span>{/if}
  {#if s.needsYes}<span class="muted"> · через «Да» в Telegram</span>{/if}
  <p class="muted">{s.desc}</p>
  <div class="row">
    {#if s.isBool}
      <select aria-label={s.key} bind:value><option value="true">true</option><option value="false">false</option></select>
    {:else}
      <input aria-label={s.key} type={s.inputType} placeholder={s.placeholder} autocomplete="off" bind:value />
    {/if}
    <button class="primary" disabled={s.secret && !value} onclick={() => send('set')}>Сохранить</button>
    {#if s.changed}<button onclick={() => send('reset')}>Сбросить</button>{/if}
  </div>
  {#if s.def}<p class="muted">Умолчание: {s.def}</p>{/if}
  {#if error}<p class="err">{error}</p>{/if}
  {#if note}<p class="ok">{note}</p>{/if}
</section>
