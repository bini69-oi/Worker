<script lang="ts">
  import type { AdminChatCode, AdminNotify } from '../lib/types.gen'
  import { api, errText } from './api'
  import { bindHint, notifyView } from './notifyView'

  let view = $state<ReturnType<typeof notifyView> | null>(null)
  let hint = $state('')
  let error = $state('')

  async function load() {
    try {
      view = notifyView(await api<AdminNotify>('GET', '/notifications'))
    } catch (e) {
      error = errText(e)
    }
  }

  async function code() {
    error = ''
    try {
      hint = bindHint((await api<AdminChatCode>('POST', '/notifications/code')).code)
    } catch (e) {
      error = errText(e)
    }
  }

  async function toggle(id: string, on: boolean) {
    error = ''
    try {
      await api('POST', '/settings/set', { key: `notify.${id}`, value: String(on) })
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Уведомления</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if view}
  <section class="box">
    <p>{view.state}</p>
    <div class="row"><button class="primary" onclick={code}>{view.bound ? 'Привязать другую группу' : 'Получить код привязки'}</button></div>
    {#if hint}<p class="ok">{hint}</p><p class="muted">Код одноразовый, новый код отменяет старый.</p>{/if}
  </section>
  <h2>Темы</h2>
  <section class="box">
    {#each view.topics as t (t.id)}
      <div class="row">
        {#if t.canToggle}
          <label><input type="checkbox" checked={t.enabled} onchange={(e) => toggle(t.id, e.currentTarget.checked)} /> {t.title}</label>
        {:else}
          <span>{t.title}</span><span class="muted">всегда включена</span>
        {/if}
      </div>
    {/each}
  </section>
{/if}
