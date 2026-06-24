import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { RabbitMQContainer, type StartedRabbitMQContainer } from "@testcontainers/rabbitmq";
import amqplib from "amqplib";
import WebSocket from "ws";
import { Hub } from "../src/hub.js";
import { connectEvents, type BrokerConnection } from "../src/broker.js";
import { createServer, type ServerHandle } from "../src/server.js";

// Skip if Docker is not available
const DOCKER_AVAILABLE = await (async () => {
  try {
    const { execSync } = await import("child_process");
    execSync("docker info", { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

describe.skipIf(!DOCKER_AVAILABLE)("notifier integration", () => {
  let rmqContainer: StartedRabbitMQContainer;
  let broker: BrokerConnection;
  let serverHandle: ServerHandle;
  let port: number;
  const EXCHANGE = "events";

  beforeAll(async () => {
    // Start RabbitMQ container
    rmqContainer = await new RabbitMQContainer("rabbitmq:3-management-alpine").start();
    const rabbitUrl = rmqContainer.getAmqpUrl();

    const hub = new Hub();
    let brokerRef: BrokerConnection | null = null;

    // Start the notifier server on a random port
    serverHandle = createServer(hub, () => brokerRef);
    await new Promise<void>((resolve) => {
      serverHandle.httpServer.listen(0, resolve);
    });
    port = (serverHandle.httpServer.address() as any).port;

    // Connect broker (consumer side)
    brokerRef = await connectEvents(rabbitUrl, (event) => {
      hub.broadcast(event as { jobId: string });
    });
    broker = brokerRef;
  }, 60_000);

  afterAll(async () => {
    if (broker) await broker.close();
    if (serverHandle) {
      serverHandle.wss.close();
      await new Promise<void>((resolve) => serverHandle.httpServer.close(() => resolve()));
    }
    if (rmqContainer) await rmqContainer.stop();
  }, 30_000);

  it("relays a published fanout event to a connected WS client", async () => {
    const rabbitUrl = rmqContainer.getAmqpUrl();

    // Connect a WebSocket client
    const ws = new WebSocket(`ws://localhost:${port}/ws`);
    await new Promise<void>((resolve, reject) => {
      ws.on("open", resolve);
      ws.on("error", reject);
    });

    // Collect received frames
    const received: object[] = [];
    ws.on("message", (data) => {
      received.push(JSON.parse(data.toString()));
    });

    // Publish an event to the fanout exchange from a separate producer connection
    const conn = await amqplib.connect(rabbitUrl);
    const ch = await conn.createChannel();
    await ch.assertExchange(EXCHANGE, "fanout", { durable: true });

    const testEvent = {
      jobId: "test-job-123",
      status: "done",
      resultKeys: { thumbnail: "processed/test_thumb.png", processed: "processed/test.png" },
      error: null,
    };

    ch.publish(EXCHANGE, "", Buffer.from(JSON.stringify(testEvent)));
    await ch.close();
    await conn.close();

    // Wait for the event to arrive (up to 5s)
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("Timeout waiting for WS frame")), 5000);
      const check = setInterval(() => {
        if (received.length > 0) {
          clearTimeout(timeout);
          clearInterval(check);
          resolve();
        }
      }, 50);
    });

    ws.close();

    expect(received.length).toBeGreaterThan(0);
    expect((received[0] as any).jobId).toBe("test-job-123");
    expect((received[0] as any).status).toBe("done");
  }, 30_000);

  it("filters events by jobId when client subscribes", async () => {
    const rabbitUrl = rmqContainer.getAmqpUrl();

    // Connect two WS clients with different subscriptions
    const wsA = new WebSocket(`ws://localhost:${port}/ws`);
    const wsB = new WebSocket(`ws://localhost:${port}/ws`);

    await Promise.all([
      new Promise<void>((r) => wsA.on("open", r)),
      new Promise<void>((r) => wsB.on("open", r)),
    ]);

    // Subscribe each to a specific jobId
    wsA.send(JSON.stringify({ subscribe: "job-A" }));
    wsB.send(JSON.stringify({ subscribe: "job-B" }));

    // Give subscription messages time to be processed
    await new Promise((r) => setTimeout(r, 100));

    const receivedA: object[] = [];
    const receivedB: object[] = [];
    wsA.on("message", (d) => receivedA.push(JSON.parse(d.toString())));
    wsB.on("message", (d) => receivedB.push(JSON.parse(d.toString())));

    // Publish event for job-A only
    const conn = await amqplib.connect(rabbitUrl);
    const ch = await conn.createChannel();
    await ch.assertExchange(EXCHANGE, "fanout", { durable: true });

    ch.publish(
      EXCHANGE,
      "",
      Buffer.from(JSON.stringify({ jobId: "job-A", status: "done", resultKeys: null, error: null }))
    );
    await ch.close();
    await conn.close();

    // Wait for delivery
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("Timeout")), 5000);
      const check = setInterval(() => {
        if (receivedA.length > 0) {
          clearTimeout(timeout);
          clearInterval(check);
          resolve();
        }
      }, 50);
    });

    // Give a bit more time for any spurious deliveries to wsB
    await new Promise((r) => setTimeout(r, 200));

    wsA.close();
    wsB.close();

    expect(receivedA.length).toBe(1);
    expect((receivedA[0] as any).jobId).toBe("job-A");
    expect(receivedB.length).toBe(0);
  }, 30_000);
});
