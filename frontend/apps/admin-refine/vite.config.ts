import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tailwindcss(), reactRouter()],
  resolve: {
    tsconfigPaths: true,
  },
  ssr: {
    // @refinedev/react-table の ESM は `lodash/isEqual`(拡張子なし)を import しており、
    // Node の厳格な ESM 解決では "Cannot find module" になる(react-router-serve 起動時に実測)。
    // バンドルに取り込んで bundler の解決に乗せる。SPA 前提のパッケージを SSR に載せる代償
    noExternal: ["@refinedev/react-table"],
  },
});
