<script lang="ts">
  // Экраны по маршруту. Во время техработ вместо них текст о работах.
  import type { Component } from 'svelte'
  import { useT } from '../lib/context'
  import { match } from '../lib/router'
  import { useApp } from './context'
  import Home from './Home.svelte'
  import Notice from './Notice.svelte'

  type Screen = Component<{ params: Record<string, string> }>
  // Образец маршрута и экран. Экраны добавляются сюда по одной строке.
  const routes: [string, Screen][] = [['/', Home as unknown as Screen]]

  const t = useT()
  const { router, me: loader } = useApp()
  const path = router.path
  const me = loader

  const found = $derived.by(() => {
    for (const [pattern, screen] of routes) {
      const params = match(pattern, $path)
      if (params) return { screen, params }
    }
    return { screen: Home as unknown as Screen, params: {} }
  })
  const maintenance = $derived($me.status === 'ready' && $me.data.maintenance)
</script>

{#if maintenance}
  <Notice title={$t('err.maintenance')} />
{:else}
  {@const Current = found.screen}
  <Current params={found.params} />
{/if}
