import { describe, it, expect } from "vitest";
import { Hub } from "../src/hub.js";

function fakeWS() {
  const sent: string[] = [];
  return { sent, readyState: 1, OPEN: 1, send: (d: string) => sent.push(d) } as any;
}

describe("Hub", () => {
  it("broadcasts to unfiltered clients", () => {
    const hub = new Hub();
    const a = fakeWS();
    hub.add(a);
    hub.broadcast({ jobId: "x", status: "done" });
    expect(JSON.parse(a.sent[0]).jobId).toBe("x");
  });

  it("respects per-client job filter", () => {
    const hub = new Hub();
    const a = fakeWS(), b = fakeWS();
    hub.add(a, "job-1");
    hub.add(b, "job-2");
    hub.broadcast({ jobId: "job-1", status: "done" });
    expect(a.sent.length).toBe(1);
    expect(b.sent.length).toBe(0);
  });
});
