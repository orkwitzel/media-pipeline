# AGENTS guidance — web

## What is tested
Pure logic in `src/api.ts` (fetch wrappers, URL builders) is unit-tested via Vitest with a stubbed `fetch`. UI components (`App.tsx`, `useJobs.ts`) are thin and not unit-tested; verify them by running `npm run build` (TypeScript errors surface here).

## Key rules
- **Never hardcode service hostnames.** Use relative paths only: `/api/...` for the gateway and `/ws` for the notifier WebSocket.
- **Routing is an Ingress concern.** nginx only serves static files with SPA fallback (`try_files $uri /index.html`). It does NOT proxy `/api` or `/ws`.
- **WebSocket URL pattern:** `ws[s]://${location.host}/ws` — protocol follows the page protocol; host comes from `location.host`.

## Adding features
- New API calls go in `src/api.ts` with a matching test in `test/api.test.ts`.
- Run `npm test && npm run build` before committing.
