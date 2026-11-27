<script lang="ts">
  // Экраны по маршруту. Во время техработ вместо них текст о работах.
  import type { Component } from 'svelte'
  import { useT } from '../lib/context'
  import { match } from '../lib/router'
  import { useApp } from './context'
  import Buy from './Buy.svelte'
  import Connect from './Connect.svelte'
  import Devices from './Devices.svelte'
  import History from './History.svelte'
  import Home from './Home.svelte'
  import Notice from './Notice.svelte'
  import Pay from './Pay.svelte'
  import Promo from './Promo.svelte'
  import Ref from './Ref.svelte'
  import Sub from './Sub.svelte'
  import Topup from './Topup.svelte'

  type Screen = Component<{ params: Record<string, string> }>
  // Образец маршрута и экран. Экраны добавляются сюда по одной строке.
  const routes: [string, Screen][] = [
    ['/', Home as unknown as Screen],
    ['/connect', Connect as unknown as Screen],
    ['/topup', Topup as unknown as Screen],
    ['/topup/:amount', Topup as unknown as Screen],
    ['/pay/:id', Pay as unknown as Screen],
    ['/sub', Sub as unknown as Screen],
    ['/devices', Devices as unknown as Screen],
    ['/ref', Ref as unknown as Screen],
    ['/promo', Promo as unknown as Screen],
    ['/history', History as unknown as Screen],
    ['/buy', Buy as unknown as Screen],
    ['/buy/:tariff', Buy as unknown as Screen],
    ['/buy/:tariff/:days', Buy as unknown as Screen],
  ]

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
  // Новый экран открывается сверху, а не на месте, где остановилась прошлая прокрутка.
  $effect(() => {
    void $path
    window.scrollTo(0, 0)
  })
  const maintenance = $derived($me.status === 'ready' && $me.data.maintenance)
</script>

{#if maintenance}
  <Notice title={$t('err.maintenance')} />
{:else}
  {@const Current = found.screen}
  <Current params={found.params} />
{/if}
