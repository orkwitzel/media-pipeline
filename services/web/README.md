# web

React + Vite SPA served by nginx on port 80.

The SPA uses **relative paths only** — it calls `/api/...` (gateway) and `/ws` (notifier WebSocket). No service hostnames are hardcoded; routing is the user's Ingress concern.

## Ingress routing (user's responsibility)

Per [`docs/contracts.md`](../../docs/contracts.md):

| Path     | Routes to          |
|----------|--------------------|
| `/`      | `web` (this service, port 80) |
| `/api/…` | `gateway` (strip `/api` prefix) |
| `/ws`    | `notifier` (WebSocket upgrade) |

## Development

```bash
npm install
npm run dev       # Vite dev server with HMR
npm test          # Vitest unit tests
npm run build     # TypeScript check + Vite production build → dist/
```

## Docker

```bash
docker build -t web .
docker run -p 8080:80 web
```

The container does **not** proxy `/api` or `/ws` — wire those through your Ingress.
