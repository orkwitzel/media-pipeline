import { loadConfig } from "./config.js";
import { Hub } from "./hub.js";
import { connectEvents, type BrokerConnection } from "./broker.js";
import { createServer } from "./server.js";

async function main() {
  const config = loadConfig();
  const hub = new Hub();

  let broker: BrokerConnection | null = null;
  const getBroker = () => broker;

  const httpServer = createServer(hub, getBroker);

  httpServer.listen(config.port, () => {
    console.log(`[notifier] listening on port ${config.port}`);
  });

  broker = await connectEvents(config.rabbitUrl, (event) => {
    hub.broadcast(event as { jobId: string });
  });

  const shutdown = async () => {
    console.log("[notifier] shutting down...");
    httpServer.close();
    if (broker) await broker.close();
    process.exit(0);
  };

  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
}

main().catch((err) => {
  console.error("[notifier] fatal error:", err);
  process.exit(1);
});
