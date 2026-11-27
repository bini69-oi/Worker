<script lang="ts">
  // История движений баланса: дата, причина, сумма со знаком.
  import Async from '../lib/Async.svelte'
  import { useT } from '../lib/context'
  import { dateTime, money } from '../lib/texts'
  import { createLoader } from '../lib/view'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const me = app.me
  const loader = createLoader(() => app.ep.history())
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')
</script>

<Screen title={$t('app.history_title')}>
  <Async view={$loader} retry={() => loader.reload()} isEmpty={(d) => d.items.length === 0} emptyKey="app.history_empty">
    {#snippet children(d)}
      <ul class="list card">
        {#each d.items as it, i (i)}
          <li>
            <div class="what">
              <span>{$t('app.reason_' + it.reason)}</span>
              <span class="hint">{dateTime(it.created_at, lang)}</span>
            </div>
            <b class="num" class:plus={it.amount > 0}>{it.amount > 0 ? '+' : '-'}{money($t, Math.abs(it.amount), lang)}</b>
          </li>
        {/each}
      </ul>
    {/snippet}
  </Async>
</Screen>

<style>
  .list {
    margin: 0;
    padding: 4px 16px;
    list-style: none;
  }
  li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 12px 0;
    border-bottom: 1px solid var(--line);
  }
  li:last-child {
    border-bottom: 0;
  }
  .what {
    display: flex;
    flex-direction: column;
  }
  .plus {
    color: var(--ok);
  }
</style>
