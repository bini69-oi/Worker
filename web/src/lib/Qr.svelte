<script lang="ts">
  // QR-код ссылки. Всегда чёрное на белом, как требует сканер, в любой теме.
  import { makeQr } from './qr'

  interface Props {
    value: string
    label: string
  }
  let { value, label }: Props = $props()
  const qr = $derived(makeQr(value))
</script>

<svg class="qr" viewBox="0 0 {qr.size} {qr.size}" role="img" aria-label={label} shape-rendering="crispEdges">
  <rect width={qr.size} height={qr.size} fill="#fff" />
  <path d={qr.path} fill="#000" />
</svg>

<style>
  .qr {
    display: block;
    width: 100%;
    height: auto;
    border-radius: 12px;
  }
</style>
