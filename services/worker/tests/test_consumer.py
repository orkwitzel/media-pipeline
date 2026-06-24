import json
import pytest
from worker.consumer import handle_job, Deps


def test_handle_job_happy_path():
    stored, published, db, cache = {}, [], {}, {}
    deps = Deps(
        get_original=lambda key: b"PNGBYTES",
        process=lambda data: (b"PROC", b"THUMB"),
        put_result=lambda key, b: stored.__setitem__(key, b),
        update_db=lambda jid, **kw: db.update({jid: kw}),
        set_cache=lambda jid, snap: cache.__setitem__(jid, snap),
        publish_event=lambda body: published.append(json.loads(body)),
    )
    msg = json.dumps({"jobId": "abc", "originalKey": "originals/abc.png", "createdAt": "now"})
    handle_job(deps, msg.encode())

    assert "processed/abc.png" in stored and "processed/abc_thumb.png" in stored
    assert db["abc"]["status"] == "done"
    assert cache["abc"]["status"] == "done"
    assert published and published[0]["status"] == "done" and published[0]["jobId"] == "abc"


def test_handle_job_failure_publishes_failed_event():
    published, db, cache = [], {}, {}

    def boom(_data):
        raise ValueError("processing exploded")

    deps = Deps(
        get_original=lambda key: b"PNGBYTES",
        process=boom,
        put_result=lambda key, b: None,
        update_db=lambda jid, **kw: db.update({jid: kw}),
        set_cache=lambda jid, snap: cache.__setitem__(jid, snap),
        publish_event=lambda body: published.append(json.loads(body)),
    )
    msg = json.dumps({"jobId": "xyz", "originalKey": "originals/xyz.png", "createdAt": "now"})

    with pytest.raises(ValueError, match="processing exploded"):
        handle_job(deps, msg.encode())

    assert published[0]["status"] == "failed"
    assert published[0]["resultKeys"] is None
    assert published[0]["error"] == "processing exploded"
    assert db["xyz"]["status"] == "failed"
    assert cache["xyz"]["status"] == "failed"
