export interface Config {
  port: number;
  rabbitUrl: string;
}

export function loadConfig(): Config {
  const rabbitUrl = process.env.RABBITMQ_URL;
  if (!rabbitUrl) throw new Error("RABBITMQ_URL environment variable is required");
  return {
    port: Number(process.env.PORT ?? 8082),
    rabbitUrl,
  };
}
