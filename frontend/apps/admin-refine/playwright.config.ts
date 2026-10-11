import { defineConfig, devices } from "@playwright/test";

const ci = !!process.env.CI;
const port = process.env.E2E_ADMIN_PORT ?? "3200";
// photo admin リスナーは既定 :8082（allinone）。ローカルのコンテナ運用では 18082 を渡す
const photoAdminUrl = process.env.PHOTO_ADMIN_URL ?? "http://localhost:8082";

export default defineConfig({
  testDir: "e2e",
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  reporter: ci ? [["github"], ["list"]] : "list",
  use: { baseURL: `http://localhost:${port}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "exec ./node_modules/.bin/react-router-serve ./build/server/index.js",
    url: `http://localhost:${port}/login`,
    env: { PORT: port, ENV: process.env.ENV ?? "dev", PHOTO_ADMIN_URL: photoAdminUrl },
    reuseExistingServer: !ci,
    gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
    timeout: 120_000,
  },
});
