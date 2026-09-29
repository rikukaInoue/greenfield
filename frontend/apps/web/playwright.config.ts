import { defineConfig, devices } from "@playwright/test";

const repoRoot = new URL("../../..", import.meta.url).pathname;
const ci = !!process.env.CI;
// ローカルで :8080 が他プロジェクトに使われていても E2E を回せるようにする。
// allinone は PHOTO_EXTERNAL_ADDR、SSR は PHOTO_API_URL を同じ値に合わせて起動すること。
const photoPort = process.env.E2E_PHOTO_PORT ?? "8080";
const webPort = process.env.E2E_WEB_PORT ?? "3000";

export default defineConfig({
  testDir: "e2e",
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  reporter: ci ? [["github"], ["list"]] : "list",
  use: { baseURL: `http://localhost:${webPort}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // exec で起動してシグナルを本体へ届ける（go run / pnpm 経由だと子が残り、テスト後に終わらない）。
  webServer: [
    {
      command: "go build -o dev/allinone/allinone.e2e ./dev/allinone && exec ./dev/allinone/allinone.e2e",
      cwd: repoRoot,
      url: `http://localhost:${photoPort}/healthz`,
      reuseExistingServer: true,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
      timeout: 180_000,
    },
    {
      // 本番と同じ build 済みの react-router-serve で起動する（build は test:e2e が先に行う）。
      // :3000 は s3admin の CORS 既定に含まれる。
      command: "exec ./node_modules/.bin/react-router-serve ./build/server/index.js",
      url: `http://localhost:${webPort}/login`,
      env: { PORT: webPort },
      reuseExistingServer: true,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
      timeout: 180_000,
    },
  ],
});
