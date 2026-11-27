<script lang="ts">
  // Экран из одного сообщения: блокировка, техработы, сбой запуска.
  import type { Snippet } from 'svelte'

  interface Props {
    title: string
    hint?: string
    children?: Snippet
  }
  let { title, hint = '', children }: Props = $props()
</script>

<main class="notice" role="alert">
  <div class="mark" aria-hidden="true"></div>
  <h1>{title}</h1>
  {#if hint}<p class="hint">{hint}</p>{/if}
  {@render children?.()}
</main>

<style>
  .notice {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    max-width: 380px;
    min-height: 100vh;
    margin: 0 auto;
    padding: 24px 24px 40px;
    justify-content: center;
    text-align: center;
  }
  .mark {
    width: 40px;
    height: 40px;
    border-radius: 12px;
    background: var(--surface-2);
    box-shadow: inset 0 0 0 2px var(--line);
  }
  h1 {
    font-size: 22px;
  }
</style>
