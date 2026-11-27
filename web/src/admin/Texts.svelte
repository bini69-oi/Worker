<script lang="ts">
  import type { AdminTextList } from '../lib/types.gen'
  import { api, errText, qs } from './api'
  import { textView } from './configView'

  let q = $state('')
  let rows = $state<ReturnType<typeof textView>[]>([])
  let draft = $state<Record<string, string>>({})
  let error = $state('')
  let note = $state('')

  const id = (key: string, lang: string) => `${key}|${lang}`

  async function load() {
    error = ''
    try {
      const res = await api<AdminTextList>('GET', '/texts' + qs({ q }))
      rows = res.items.map(textView)
      for (const r of rows) {
        draft[id(r.key, 'ru')] = r.ru
        draft[id(r.key, 'en')] = r.en
      }
    } catch (e) {
      error = errText(e)
    }
  }

  async function save(key: string, lang: string) {
    error = ''
    note = ''
    try {
      await api('POST', '/texts/set', { key, lang, value: draft[id(key, lang)] ?? '' })
      note = 'Сохранено, бот и кабинет покажут новый текст в течение 5 секунд'
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  async function reset(key: string, lang: string) {
    error = ''
    try {
      await api('POST', '/texts/reset', { key, lang })
      note = 'Сброшено к умолчанию'
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Тексты</h1>
<form class="row" onsubmit={(e) => { e.preventDefault(); void load() }}>
  <input placeholder="Поиск по ключу или тексту" aria-label="Поиск" bind:value={q} />
  <button class="primary">Найти</button>
</form>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}

{#each rows as r (r.key)}
  <section class="box">
    <h2>{r.key}</h2>
    <p class="muted">Подстановки: {r.params.length ? r.params.join(' ') : 'нет'}. До {r.limit} знаков.</p>
    {#each [['ru', r.changedRu, r.defaultRu], ['en', r.changedEn, r.defaultEn]] as [lang, changed, def] (lang)}
      <label for={id(r.key, String(lang))}>{lang}{changed ? ' (изменён)' : ''}</label>
      <textarea id={id(r.key, String(lang))} maxlength={r.limit} bind:value={draft[id(r.key, String(lang))]}></textarea>
      <p class="muted">Умолчание: {def}</p>
      <div class="row">
        <button class="primary" onclick={() => save(r.key, String(lang))}>Сохранить</button>
        {#if changed}<button onclick={() => reset(r.key, String(lang))}>Сбросить</button>{/if}
      </div>
    {/each}
  </section>
{/each}
{#if rows.length === 0 && !error}<p class="muted">Ничего не нашли.</p>{/if}
