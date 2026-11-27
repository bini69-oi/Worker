<script lang="ts" generics="T">
  // Обёртка экрана: показывает загрузку, ошибку с кодом или «Нет связи», а когда данные готовы, содержимое.
  // Пустое состояние задаёт сам экран: isEmpty говорит, что данных нет, и тогда виден empty вместо содержимого.
  import type { Snippet } from 'svelte'
  import { useT } from './context'
  import type { View } from './view'

  interface Props {
    view: View<T>
    retry: () => void
    /** Данные пришли, но показывать нечего. */
    isEmpty?: (data: T) => boolean
    /** Текст пустого экрана по ключу. */
    emptyKey?: string
    children: Snippet<[T]>
  }
  let { view, retry, isEmpty, emptyKey = 'app.empty', children }: Props = $props()
  const t = useT()
</script>

{#if view.status === 'loading'}
  <div class="state" role="status" aria-live="polite">
    <span class="spinner" aria-hidden="true"></span>
    <p>{$t('app.loading')}</p>
  </div>
{:else if view.status === 'offline'}
  <div class="state" role="alert">
    <p class="state-title">{$t('app.offline')}</p>
    <p class="hint">{$t('app.offline_hint')}</p>
    <button class="btn" type="button" onclick={retry}>{$t('app.retry')}</button>
  </div>
{:else if view.status === 'error'}
  <div class="state" role="alert">
    <p class="state-title">{view.text || $t('err.internal')}</p>
    {#if view.code}<p class="hint code">{$t('app.error_code', { code: view.code })}</p>{/if}
    <button class="btn" type="button" onclick={retry}>{$t('app.retry')}</button>
  </div>
{:else if isEmpty?.(view.data)}
  <div class="state">
    <p class="hint">{$t(emptyKey)}</p>
  </div>
{:else}
  {@render children(view.data)}
{/if}

<style>
  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 48px 8px;
    text-align: center;
  }
  .state-title {
    margin: 0;
    font-size: 17px;
    font-weight: 600;
  }
  .state p {
    margin: 0;
  }
  .code {
    font-family: var(--mono);
    font-size: 13px;
  }
  .spinner {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    border: 3px solid var(--line);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 3s;
    }
  }
</style>
