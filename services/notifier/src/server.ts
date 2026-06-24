import http from "http";
import { WebSocketServer } from "ws";
import type { Hub } from "./hub.js";
import type { BrokerConnection } from "./broker.js";

export function createServer(
  hub: Hub,
  getBroker: () => BrokerConnection | null
): http.Server {
  const httpServer = http.createServer((req, res) => {
    if (req.url === "/healthz") {
      res.writeHead(200, { "Content-Type": "text/plain" });
      res.end("ok");
      return;
    }
    if (req.url === "/readyz") {
      const broker = getBroker();
      const ready = broker !== null && broker.isReady();
      res.writeHead(ready ? 200 : 503, { "Content-Type": "text/plain" });
      res.end(ready ? "ok" : "not ready");
      return;
    }
    res.writeHead(404);
    res.end();
  });

  const wss = new WebSocketServer({ server: httpServer, path: "/ws" });

  wss.on("connection", (ws) => {
    const client = hub.add(ws);

    ws.on("message", (data) => {
      try {
        const msg = JSON.parse(data.toString());
        if (typeof msg.subscribe === "string") {
          client.filter = msg.subscribe;
        }
      } catch {
        // ignore malformed messages
      }
    });

    ws.on("close", () => {
      hub.remove(ws);
    });

    ws.on("error", () => {
      hub.remove(ws);
    });
  });

  return httpServer;
}
