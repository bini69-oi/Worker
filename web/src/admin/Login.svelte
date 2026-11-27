<script lang="ts">
  import { api, errText } from './api'
  import { runLogin } from './loginFlow'
  import { loadMe } from './session.svelte'

  let tg = $state('')
  let password = $state('')
  let code = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e: Event) {
    e.preventDefault()
    error = ''
    code = ''
    const id = Number(tg)
    if (!Number.isSafeInteger(id) || id <= 0 || !password) {
      error = 'Введите Telegram ID и пароль'
      return
    }
    busy = true
    try {
      const res = await runLogin(api, id, password, (c) => (code = c))
      if (res === 'ok') await loadMe()
      else error = res === 'denied' ? 'Вход отклонён в Telegram' : 'Время на подтверждение вышло'
    } catch (err) {
      error = errText(err)
    } finally {
      busy = false
      password = ''
      code = ''
    }
  }
</script>

<main>
  <h1>Вход в админку</h1>
  <section class="box">
    <form onsubmit={submit}>
      <div class="row"><label for="tg">Telegram ID</label><input id="tg" inputmode="numeric" bind:value={tg} /></div>
      <div class="row"><label for="pw">Пароль</label><input id="pw" type="password" autocomplete="current-password" bind:value={password} /></div>
      <div class="row"><button class="primary" disabled={busy}>Войти</button></div>
    </form>
    {#if code}
      <p>В Telegram вам пришло сообщение о входе. Нажмите кнопку с этим кодом:</p>
      <div class="code">{code}</div>
    {/if}
    {#if error}<p class="err">{error}</p>{/if}
  </section>
</main>
