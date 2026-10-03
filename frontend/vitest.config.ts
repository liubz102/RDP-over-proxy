import { defineConfig } from "vitest/config";

// Kept apart from vite.config.ts so unit tests don't load the Wails plugin.
export default defineConfig({
  test: {
    include: ["tests/**/*.test.{ts,tsx}"],
    environment: "node",
  },
});
