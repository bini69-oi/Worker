<script lang="ts">
  import type { AdminUserCard } from '../lib/types.gen'
  import { api, errText } from './api'
  import { dt, parseRub, rub } from './format'
  import { cardView } from './usersView'

  let { id }: { id: number } = $props()

  let card = $state<AdminUserCard | null>(null)
  let error = $state('')
  let note = $state('')
  let amount = $state('')
  let comment = $state('')
  let days = $state('30')
  let text = $state('')

  const view = $derived(card ? cardView(card) : null)

  async function load() {
    try {
      card = await api<AdminUserCard>('GET', `/users/${id}`)
    } catch (e) {
      error = errText(e)
    }
  }

  // act выполняет действие, показывает итог и перечитывает карточку.
  async function act(path: string, body: unknown, done: string) {
    error = ''
    note = ''
    try {
      await api('POST', `/users/${id}/${path}`, body)
      note = done
      await load()
    } catch (e) {
      error = errText(e)
    }
  }

  function money(sign: 1 | -1) {
    const kop = parseRub(amount)
    if (kop === null || kop <= 0) {
      error = 'Сумма: число рублей, например 100 или 50,5'
      return
    }
    void act('balance', { amount: sign * kop, comment }, sign > 0 ? 'Начислено' : 'Списано')
  }

  $effect(() => {
    void load()
  })
</script>

{#if error}<p class="err">{error}</p>{/if}
{#if note}<p class="ok">{note}</p>{/if}
{#if card && view}
  <h1>{view.title}</h1>
  <section class="box">
    <dl class="kv">
      <dt>Telegram ID</dt><dd>{card.tg_id}</dd>
      <dt>Язык</dt><dd>{card.lang}</dd>
      <dt>Регистрация</dt><dd>{dt(card.created_at)}</dd>
      <dt>Пригласил</dt><dd>{card.referrer_tg || '—'}</dd>
      <dt>Баланс</dt><dd>{view.balance}</dd>
      {#if view.flags.length}<dt>Пометки</dt><dd class="err">{view.flags.join(', ')}</dd>{/if}
    </dl>
  </section>

  <h2>Подписка</h2>
  <section class="box">
    {#if view.sub}
      <dl class="kv">
        <dt>Тариф</dt><dd>{view.sub.tariff}</dd>
        <dt>До</dt><dd>{view.sub.until}</dd>
        <dt>Трафик</dt><dd>{view.sub.traffic}</dd>
        <dt>Устройств</dt><dd>{view.sub.devices}</dd>
        <dt>Автопродление</dt><dd>{view.sub.autoRenew}</dd>
      </dl>
    {:else}
      <p class="muted">Подписки нет.</p>
    {/if}
  </section>

  <h2>Устройства</h2>
  <section class="box">
    {#if view.devices === null}
      <p class="err">{view.devicesNote}</p>
    {:else if view.devices.length === 0}
      <p class="muted">Нет устройств.</p>
    {:else}
      <ul>{#each view.devices as d (d)}<li>{d}</li>{/each}</ul>
    {/if}
    <div class="row">
      <button onclick={() => act('devices/reset', {}, 'Устройства сброшены')}>Сбросить устройства</button>
      <button onclick={() => act('link/revoke', {}, 'Ссылка перевыпущена')}>Перевыпустить ссылку</button>
    </div>
  </section>

  <h2>Действия</h2>
  <section class="box">
    <div class="row">
      <label for="amount">Сумма, ₽</label><input id="amount" size="8" inputmode="decimal" bind:value={amount} />
      <label for="comment">Комментарий</label><input id="comment" bind:value={comment} />
      <button onclick={() => money(1)}>Начислить</button>
      <button class="danger" onclick={() => money(-1)}>Списать</button>
    </div>
    <div class="row">
      <label for="days">Дней</label><input id="days" size="4" inputmode="numeric" bind:value={days} />
      <button onclick={() => act('days', { days: Number(days) }, 'Дни выданы')}>Выдать дни</button>
    </div>
    <div class="row">
      {#if card.banned}
        <button onclick={() => act('unban', { comment }, 'Разбанен')}>Разбанить</button>
      {:else}
        <button class="danger" onclick={() => act('ban', { comment }, 'Забанен')}>Забанить</button>
      {/if}
      <span class="muted">нужен комментарий выше</span>
    </div>
    <label for="msg">Сообщение абоненту</label>
    <textarea id="msg" bind:value={text}></textarea>
    <div class="row"><button class="primary" onclick={() => act('message', { text }, 'Отправлено')}>Отправить</button></div>
  </section>

  <h2>Движения денег</h2>
  <table class="tbl">
    <thead><tr><th>Когда</th><th>Сумма</th><th>Причина</th><th>Комментарий</th></tr></thead>
    <tbody>
      {#each card.ledger as e (e.created_at + e.reason + e.amount)}
        <tr><td data-label="Когда">{dt(e.created_at)}</td><td data-label="Сумма">{rub(e.amount)}</td><td data-label="Причина">{e.reason}</td><td data-label="Комментарий">{e.comment}</td></tr>
      {/each}
    </tbody>
  </table>

  <h2>Платежи</h2>
  <table class="tbl">
    <thead><tr><th>Когда</th><th>Касса</th><th>Сумма</th><th>Статус</th></tr></thead>
    <tbody>
      {#each card.payments as p (p.id)}
        <tr><td data-label="Когда">{dt(p.created_at)}</td><td data-label="Касса">{p.gateway}</td><td data-label="Сумма">{rub(p.amount)}</td><td data-label="Статус">{p.status}</td></tr>
      {/each}
    </tbody>
  </table>

  <h2>Журнал по абоненту</h2>
  <table class="tbl">
    <thead><tr><th>Когда</th><th>Админ</th><th>Действие</th></tr></thead>
    <tbody>
      {#each card.audit as a (a.id)}
        <tr><td data-label="Когда">{dt(a.at)}</td><td data-label="Админ">{a.admin_tg}</td><td data-label="Действие">{a.action}</td></tr>
      {/each}
    </tbody>
  </table>
{/if}
