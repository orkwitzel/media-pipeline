import { useRef, useState } from "react";
import { uploadFile, resultURL } from "./api";
import { useJobs } from "./useJobs";

export function App() {
  const fileRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const jobs = useJobs();

  async function handleUpload() {
    const file = fileRef.current?.files?.[0];
    if (!file) return;
    setUploading(true);
    setError(null);
    try {
      await uploadFile(file);
      if (fileRef.current) fileRef.current.value = "";
    } catch (e) {
      setError(e instanceof Error ? e.message : "upload failed");
    } finally {
      setUploading(false);
    }
  }

  return (
    <div style={{ fontFamily: "sans-serif", maxWidth: 800, margin: "0 auto", padding: 24 }}>
      <h1>Media Pipeline</h1>

      <div style={{ marginBottom: 24 }}>
        <input ref={fileRef} type="file" accept="image/*,video/*" />
        <button onClick={handleUpload} disabled={uploading} style={{ marginLeft: 8 }}>
          {uploading ? "Uploading…" : "Upload"}
        </button>
        {error && <span style={{ color: "red", marginLeft: 8 }}>{error}</span>}
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))", gap: 16 }}>
        {jobs.map((job) => (
          <div key={job.id} style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
            <div style={{ fontSize: 12, color: "#666", marginBottom: 8 }}>
              {job.id.slice(0, 8)}… — <strong>{job.status}</strong>
            </div>
            {job.status === "done" ? (
              <a href={resultURL(job.id, "processed")} target="_blank" rel="noreferrer">
                <img
                  src={resultURL(job.id, "thumbnail")}
                  alt={`thumbnail for ${job.id}`}
                  style={{ width: "100%", borderRadius: 4 }}
                />
              </a>
            ) : (
              <div style={{ height: 120, background: "#f5f5f5", borderRadius: 4, display: "flex", alignItems: "center", justifyContent: "center", color: "#aaa" }}>
                {job.status === "failed" ? (job.error ?? "failed") : "processing…"}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
