import json
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Callable


@dataclass
class Deps:
    get_original: Callable[[str], bytes]
    process: Callable[[bytes], tuple[bytes, bytes]]
    put_result: Callable[[str, bytes], None]
    update_db: Callable[..., None]
    set_cache: Callable[[str, dict], None]
    publish_event: Callable[[bytes], None]


def handle_job(deps: Deps, body: bytes) -> None:
    msg = json.loads(body)
    jid = msg["jobId"]
    original_key = msg["originalKey"]
    created_at = msg["createdAt"]
    try:
        original = deps.get_original(original_key)
        processed, thumb = deps.process(original)
        pkey, tkey = f"processed/{jid}.png", f"processed/{jid}_thumb.png"
        deps.put_result(pkey, processed)
        deps.put_result(tkey, thumb)
        deps.update_db(jid, status="done", processed_key=pkey, thumbnail_key=tkey, error=None)
        updated_at = datetime.now(timezone.utc).isoformat()
        snap = {
            "id": jid,
            "status": "done",
            "originalKey": original_key,
            "thumbnailKey": tkey,
            "processedKey": pkey,
            "error": None,
            "createdAt": created_at,
            "updatedAt": updated_at,
        }
        deps.set_cache(jid, snap)
        deps.publish_event(json.dumps({"jobId": jid, "status": "done",
            "resultKeys": {"thumbnail": tkey, "processed": pkey}, "error": None}).encode())
    except Exception as exc:  # noqa: BLE001 — convert any failure into a failed event
        deps.update_db(jid, status="failed", error=str(exc))
        updated_at = datetime.now(timezone.utc).isoformat()
        snap = {
            "id": jid,
            "status": "failed",
            "originalKey": original_key,
            "thumbnailKey": None,
            "processedKey": None,
            "error": str(exc),
            "createdAt": created_at,
            "updatedAt": updated_at,
        }
        deps.set_cache(jid, snap)
        deps.publish_event(json.dumps({"jobId": jid, "status": "failed",
            "resultKeys": None, "error": str(exc)}).encode())
        raise
