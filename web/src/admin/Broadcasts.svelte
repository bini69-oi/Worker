<script lang="ts">
  import type { AdminBroadcast, AdminBroadcastList, AdminSegmentCount } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { bcView, segments } from './broadcastView'

  let segment = $state('all')
  let count = $state<number | null>(null)
  let text = $state('')
  let buttonText = $state('')
  let buttonUrl = $state('')
  let rows = $state<ReturnType<typeof bcView>[]>([])
  let error = $state('')
  let note = $state('')

  async function load() {
    try {
      const res = await api<AdminBroadcastList>('GET', '/broadcasts')
      rows = res.items.map(bcView)
    } catch (e) {
      error = errText(e)
    }
  }

  async function loadCount() {
    count = null
    try {
      count = (await api<AdminSegmentCount>('GET', '/broadcasts/segment' + qs({ segment }))).count
    } catch (e) {
      error = errText(e)
    }
  }

  async function create(e: Event) {
    e.preventDefault()
    error = ''
    note = ''
    try {
      await api<AdminBroadcast>('POST', '/broadcasts', { segment, text, button_text: buttonText, button_url: buttonUrl })
      note = 'Черновик сохранён: проверьте его на себе и запустите'
      text = ''
      await load()
    } catch (err) {
      error = errText(err)
    }
  }

  async function act(id: number, what: 'test' | 'start' | 'cancel', done: string) {
    error = ''
    note = ''
    try {
      await api('POST', `/broadcasts/${id}/${what}`)
      note = done
    } catch (e) {
      error = errText(e)
    }
    await load()
  }

  $effect(() => {
    void load()
    // Пока рассылка идёт, прогресс обновляется раз в 2 секунды. Таймер снимается вместе с экраном.
    const timer = setInterval(() => {
      if (rows.some((r) => r.running)) void load()
    }, 2000)
    return () => clearInterval(timer)
  })
  $effect(() => {
    void segment
    void loadCount()
  })
</script>

<h1>Рассылки</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}

<section class="box">
  <form onsubmit={create}>
    <div class="row">
      <label for="seg">Кому</label>
      <select id="seg" bind:value={segment}>{#each segments as s (s.id)}<option value={s.id}>{s.title}</option>{/each}</select>
      <span class="muted">получателей: {count ?? '…'}</span>
    </div>
    <label for="txt">Текст (разметка Telegram: b, i, u, s, a, code, pre, blockquote)</label>
    <textarea id="txt" maxlength="4096" bind:value={text}></textarea>
    <div class="row">
      <input placeholder="Подпись кнопки" aria-label="Подпись кнопки" maxlength="64" bind:value={buttonText} />
      <input placeholder="https://…" aria-label="Ссылка кнопки" bind:value={buttonUrl} />
    </div>
    <div class="row"><button class="primary">Сохранить черновик</button></div>
  </form>
</section>

<table class="tbl">
  <thead><tr><th>Создана</th><th>Кому</th><th>Текст</th><th>Статус</th><th>Прогресс</th><th></th></tr></thead>
  <tbody>
    {#each rows as r (r.id)}
      <tr>
        <td data-label="Создана">{r.created}</td>
        <td data-label="Кому">{r.segment}</td>
        <td data-label="Текст">{r.text}</td>
        <td data-label="Статус">{r.status}</td>
        <td data-label="Прогресс">
          <progress max="100" value={r.progress}></progress> {r.progress}%
          <div class="muted">{r.counts}</div>
        </td>
        <td>
          {#if r.canTest}<button onclick={() => act(r.id, 'test', 'Отправлено вам в Telegram')}>Отправить себе</button>{/if}
          {#if r.canStart}<button class="primary" onclick={() => act(r.id, 'start', 'Рассылка запущена')}>Запустить</button>{/if}
          {#if r.canCancel}<button class="danger" onclick={() => act(r.id, 'cancel', 'Рассылка отменена')}>Отменить</button>{/if}
        </td>
      </tr>
    {/each}
  </tbody>
</table>
