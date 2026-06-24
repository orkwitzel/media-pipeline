import { describe, it, expect, vi } from "vitest";
import { uploadFile } from "../src/api";

describe("uploadFile", () => {
  it("POSTs to /api/upload and returns jobId", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 202, json: async () => ({ jobId: "job-9" }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const id = await uploadFile(new File(["x"], "a.png", { type: "image/png" }));
    expect(id).toBe("job-9");
    expect(fetchMock).toHaveBeenCalledWith("/api/upload", expect.objectContaining({ method: "POST" }));
  });
});
