import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    testTimeout: 60_000,
    hookTimeout: 60_000,
    // Required for testcontainers with Rancher Desktop / non-standard docker sockets
    env: {
      TESTCONTAINERS_RYUK_DISABLED: "true",
    },
  },
});
