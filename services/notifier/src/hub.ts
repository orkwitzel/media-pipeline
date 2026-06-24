import type { WebSocket } from "ws";

type Client = { ws: WebSocket; filter?: string };

export class Hub {
  private clients = new Set<Client>();

  add(ws: WebSocket, filter?: string): Client {
    const c: Client = { ws, filter };
    this.clients.add(c);
    return c;
  }
  remove(ws: WebSocket): void {
    for (const c of this.clients) if (c.ws === ws) this.clients.delete(c);
  }
  broadcast(event: { jobId: string; [k: string]: unknown }): void {
    const data = JSON.stringify(event);
    for (const c of this.clients) {
      if (c.filter && c.filter !== event.jobId) continue;
      if ((c.ws as any).readyState === (c.ws as any).OPEN) c.ws.send(data);
    }
  }
  get size(): number { return this.clients.size; }
}
