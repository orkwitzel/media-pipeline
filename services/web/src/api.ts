export type Job = {
  id: string; status: string;
  originalKey: string | null;
  thumbnailKey: string | null; processedKey: string | null; error: string | null;
  createdAt: string; updatedAt: string;
};

export async function uploadFile(file: File): Promise<string> {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch("/api/upload", { method: "POST", body: fd });
  if (!res.ok) throw new Error(`upload failed: ${res.status}`);
  return (await res.json()).jobId as string;
}

export async function listJobs(): Promise<Job[]> {
  const res = await fetch("/api/jobs");
  if (!res.ok) throw new Error(`list failed: ${res.status}`);
  return res.json();
}

export function resultURL(id: string, variant: "thumbnail" | "processed"): string {
  return `/api/jobs/${id}/result?variant=${variant}`;
}
