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
  // CI では前段のステップで起動済みのものを使う（Playwright に子プロセスを持たせるとジョブが終わらない）。
  webServer: [
    {
      command: "go run ./dev/allinone",
      cwd: repoRoot,
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      timeout: 180_000,
    },
    {
      // 本番と同じ build + react-router-serve で起動する。:3000 は s3admin の CORS 既定に含まれる。
      command: "pnpm build && pnpm start",
      url: "http://localhost:3000/login",
      env: { PORT: "3000" },
      reuseExistingServer: true,
      timeout: 180_000,
    },
  ],
});
