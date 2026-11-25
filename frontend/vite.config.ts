import { defineConfig } from 'vitest/config';

// During `npm run dev`, forward /ws and /api to the dashboard running in the kind cluster.
const dashboard = process.env.DASHBOARD_URL ?? 'http://localhost:30080';

export default defineConfig({
  server: {
    proxy: {
      '/ws': { target: dashboard, ws: true },
      '/api': { target: dashboard },
    },
  },
  test: { environment: 'jsdom' },
});
