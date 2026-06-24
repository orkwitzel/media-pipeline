import amqplib, { type ChannelModel, type ConsumeMessage } from "amqplib";

const EXCHANGE = "events";
const RETRY_DELAY_MS = 2000;
const MAX_RETRIES = 10;

export interface BrokerConnection {
  isReady: () => boolean;
  close: () => Promise<void>;
}

export async function connectEvents(
  url: string,
  onEvent: (event: object) => void
): Promise<BrokerConnection> {
  let channelModel: ChannelModel | null = null;
  let ready = false;

  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    try {
      channelModel = await amqplib.connect(url);
      break;
    } catch (err) {
      if (attempt === MAX_RETRIES) throw err;
      console.warn(`[broker] connect attempt ${attempt + 1} failed, retrying in ${RETRY_DELAY_MS}ms...`);
      await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
    }
  }

  if (!channelModel) throw new Error("Failed to connect to RabbitMQ");

  const channel = await channelModel.createChannel();

  // Assert the fanout exchange (durable, matches worker)
  await channel.assertExchange(EXCHANGE, "fanout", { durable: true });

  // Assert an exclusive, auto-delete anonymous queue — one per replica
  const { queue } = await channel.assertQueue("", {
    exclusive: true,
    autoDelete: true,
    durable: false,
  });

  // Bind queue to the fanout exchange (no routing key needed for fanout)
  await channel.bindQueue(queue, EXCHANGE, "");

  await channel.consume(queue, (msg: ConsumeMessage | null) => {
    if (!msg) return;
    try {
      const event = JSON.parse(msg.content.toString("utf8")) as object;
      onEvent(event);
    } catch (err) {
      console.error("[broker] failed to parse message:", err);
    }
    channel.ack(msg);
  });

  ready = true;
  console.log(`[broker] connected to ${EXCHANGE} exchange, queue: ${queue}`);

  return {
    isReady: () => ready,
    close: async () => {
      ready = false;
      try { await channel.close(); } catch {}
      try { await channelModel!.close(); } catch {}
    },
  };
}
