<script lang="ts">
  import type { AdminSessionList, AdminSettingResult, AdminTeam } from '../lib/types.gen'
  import { api, errText } from './api'
  import { memberView, permTitles, sessionView } from './teamView'

  let members = $state<ReturnType<typeof memberView>[]>([])
  let sessions = $state<ReturnType<typeof sessionView>[]>([])
  let error = $state('')
  let note = $state('')
  let tg = $state('')
  let password = $state('')
  let chosen = $state<string[]>([])

  async function load() {
    try {
      members = (await api<AdminTeam>('GET', '/team')).items.map(memberView)
      sessions = (await api<AdminSessionList>('GET', '/sessions')).items.map(sessionView)
    } catch (e) {
      error = errText(e)
    }
  }

  async function add(e: Event) {
    e.preventDefault()
    error = ''
    note = ''
    const id = Number(tg)
    if (!Number.isSafeInteger(id) || id <= 0) {
      error = 'Telegram ID: целое число'
      return
    }
    try {
      await api<AdminSettingResult>('POST', '/team/add', { tg_id: id, password, perms: chosen })
      note = 'Подтвердите в Telegram: помощник появится после «Да»'
      password = ''
    } catch (err) {
      error = errText(err)
    }
  }

  async function remove(id: number) {
    error = ''
    try {
      await api('POST', '/team/remove', { tg_id: id })
      note = 'Подтвердите в Telegram: помощник будет отключён после «Да»'
    } catch (e) {
      error = errText(e)
    }
  }

  async function closeSession(id: string) {
    error = ''
    try {
      await api('POST', '/sessions/close', { id })
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  $effect(() => {
    void load()
  })
</script>

<h1>Команда</h1>
{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}

<table class="tbl">
  <thead><tr><th>Telegram ID</th><th>Роль</th><th>Права</th><th></th></tr></thead>
  <tbody>
    {#each members as m (m.tg)}
      <tr>
        <td data-label="Telegram ID">{m.tg}</td>
        <td data-label="Роль">{m.role}</td>
        <td data-label="Права">{m.perms}</td>
        <td>{#if m.canRemove}<button class="danger" onclick={() => remove(m.tg)}>Убрать</button>{/if}</td>
      </tr>
    {/each}
  </tbody>
</table>

<h2>Новый помощник</h2>
<section class="box">
  <form onsubmit={add}>
    <div class="row"><label for="htg">Telegram ID</label><input id="htg" inputmode="numeric" bind:value={tg} /></div>
    <div class="row"><label for="hpw">Пароль, от 10 знаков</label><input id="hpw" type="password" autocomplete="new-password" bind:value={password} /></div>
    {#each Object.entries(permTitles) as [id, title] (id)}
      <div><label><input type="checkbox" value={id} bind:group={chosen} /> {title}</label></div>
    {/each}
    <div class="row"><button class="primary">Добавить</button><span class="muted">пароль владельца здесь не меняется</span></div>
  </form>
</section>

<h2>Открытые сессии</h2>
<table class="tbl">
  <thead><tr><th>Админ</th><th>IP</th><th>Браузер</th><th>Последний запрос</th><th></th></tr></thead>
  <tbody>
    {#each sessions as s (s.id)}
      <tr>
        <td data-label="Админ">{s.admin}{s.current ? ' (вы)' : ''}</td>
        <td data-label="IP">{s.ip}</td>
        <td data-label="Браузер">{s.agent}</td>
        <td data-label="Последний запрос">{s.seen}</td>
        <td><button onclick={() => closeSession(s.id)}>Закрыть</button></td>
      </tr>
    {/each}
  </tbody>
</table>
