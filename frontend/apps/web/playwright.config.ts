import { defineConfig, devices } from "@playwright/test";

const repoRoot = new URL("../../..", import.meta.url).pathname;
const ci = !!process.env.CI;

export default defineConfig({
  testDir: "e2e",
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  reporter: ci ? [["github"], ["list"]] : "list",
  use: { baseURL: "http://localhost:3000", trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // exec で起動してシグナルを本体へ届ける（go run / pnpm 経由だと子が残り、テスト後に終わらない）。
  webServer: [
    {
      command: "go build -o dev/allinone/allinone.e2e ./dev/allinone && exec ./dev/allinone/allinone.e2e",
      cwd: repoRoot,
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
      timeout: 180_000,
    },
    {
      // 本番と同じ build 済みの react-router-serve で起動する（build は test:e2e が先に行う）。
      // :3000 は s3admin の CORS 既定に含まれる。
      command: "exec ./node_modules/.bin/react-router-serve ./build/server/index.js",
      url: "http://localhost:3000/login",
      env: { PORT: "3000" },
      reuseExistingServer: true,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
      timeout: 180_000,
    },
  ],
});
