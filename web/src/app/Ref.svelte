<script lang="ts">
  // Рефералка: ссылка, сколько пришло и купило, сколько заработано.
  import Async from '../lib/Async.svelte'
  import { useT } from '../lib/context'
  import { openChat } from '../lib/telegram'
  import { money } from '../lib/texts'
  import { createLoader } from '../lib/view'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const { ep, tg, win } = app
  const me = app.me
  const loader = createLoader(() => ep.ref())
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')
  let copied = $state(false)

  async function copy(link: string) {
    try {
      await win.navigator.clipboard.writeText(link)
      copied = true
      setTimeout(() => (copied = false), 2000)
    } catch {
      copied = false
    }
  }
</script>

<Screen title={$t('app.ref_title')}>
  <Async view={$loader} retry={() => loader.reload()}>
    {#snippet children(r)}
      <p class="hint">{$t('app.ref_hint')}</p>
      <div class="card stack">
        <code class="link">{r.link}</code>
        <button class="btn wide" type="button" onclick={() => copy(r.link)}>{copied ? $t('app.connect_copied') : $t('app.connect_copy')}</button>
        <button class="btn wide soft" type="button" onclick={() => openChat(tg, win, 'https://t.me/share/url?url=' + encodeURIComponent(r.link))}>{$t('app.ref_share')}</button>
      </div>
      <dl class="stats">
        <div class="card"><dd class="num">{r.invited}</dd><dt>{$t('app.ref_invited')}</dt></div>
        <div class="card"><dd class="num">{r.bought}</dd><dt>{$t('app.ref_bought')}</dt></div>
        <div class="card"><dd class="num">{money($t, r.earned, lang)}</dd><dt>{$t('app.ref_earned')}</dt></div>
      </dl>
    {/snippet}
  </Async>
</Screen>

<style>
  .link {
    font: 500 14px/1.4 var(--mono);
    overflow-wrap: anywhere;
    user-select: all;
  }
  .stats {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    margin: 0;
  }
  .stats .card {
    padding: 12px;
    display: flex;
    flex-direction: column-reverse;
    gap: 2px;
  }
  dd {
    margin: 0;
    font-size: 20px;
    font-weight: 700;
  }
  dt {
    font-size: 12px;
    color: var(--hint);
  }
</style>
