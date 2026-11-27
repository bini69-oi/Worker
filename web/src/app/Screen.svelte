<script lang="ts">
  // Рамка экрана: заголовок и «Назад» в браузере. Внутри Telegram «Назад» рисует сам Telegram.
  import type { Snippet } from 'svelte'
  import { useT } from '../lib/context'
  import { useApp } from './context'

  interface Props {
    title: string
    children: Snippet
  }
  let { title, children }: Props = $props()
  const t = useT()
  const { router, tg } = useApp()
</script>

<main class="screen">
  <header>
    {#if !tg}
      <button class="back" type="button" aria-label={$t('app.back')} onclick={() => router.back()}>
        <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M15 5l-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
    {/if}
    <h1>{title}</h1>
  </header>
  <div class="body stack">
    {@render children()}
  </div>
</main>

<style>
  .screen {
    max-width: 520px;
    margin: 0 auto;
    padding: max(16px, env(safe-area-inset-top)) 16px max(32px, env(safe-area-inset-bottom));
  }
  header {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 48px;
    margin: 0 0 14px -8px;
  }
  h1 {
    font-size: 24px;
    margin-left: 8px;
  }
  header:has(.back) h1 {
    margin-left: 0;
  }
  .back {
    appearance: none;
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: var(--text);
    cursor: pointer;
  }
  .back:active {
    background: var(--surface-2);
  }
</style>
