import { useState, useEffect } from "react";
import { listJobs, type Job } from "./api";

export function useJobs(): Job[] {
  const [jobs, setJobs] = useState<Job[]>([]);

  useEffect(() => {
    listJobs().then(setJobs).catch(console.error);

    const proto = location.protocol === "https:" ? "wss" : "ws";
    const wsUrl = `${proto}://${location.host}/ws`;
    let ws: WebSocket;
    let closed = false;

    function connect() {
      ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        listJobs().then(setJobs).catch(console.error);
      };

      ws.onmessage = (evt) => {
        try {
          const event = JSON.parse(evt.data) as { jobId: string; status: string; resultKeys: { thumbnail: string; processed: string } | null; error: string | null };
          setJobs((prev) => {
            const idx = prev.findIndex((j) => j.id === event.jobId);
            const updated: Job = {
              id: event.jobId,
              status: event.status,
              thumbnailKey: event.resultKeys?.thumbnail ?? null,
              processedKey: event.resultKeys?.processed ?? null,
              error: event.error,
              createdAt: idx >= 0 ? prev[idx].createdAt : new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            };
            if (idx >= 0) {
              const next = [...prev];
              next[idx] = updated;
              return next;
            }
            return [updated, ...prev];
          });
        } catch {
          // ignore malformed frames
        }
      };

      ws.onclose = () => {
        if (!closed) setTimeout(connect, 2000);
      };
    }

    connect();

    return () => {
      closed = true;
      ws?.close();
    };
  }, []);

  return jobs;
}
