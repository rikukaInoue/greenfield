import type { Config } from "@react-router/dev/config";

export default {
  // SSR で返す。refine のデータ取得はブラウザ側のフックが行うので、初期 HTML は骨格のみ（比較の論点）
  ssr: true,
} satisfies Config;
