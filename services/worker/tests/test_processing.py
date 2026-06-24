import io
from PIL import Image
from worker.processing import process_image

def _png(w, h):
    buf = io.BytesIO()
    Image.new("RGB", (w, h), (10, 120, 200)).save(buf, "PNG")
    return buf.getvalue()

def test_process_image_returns_processed_and_thumbnail():
    processed, thumb = process_image(_png(2000, 1000), thumb_px=256, max_px=1280, watermark="hi")
    p = Image.open(io.BytesIO(processed))
    t = Image.open(io.BytesIO(thumb))
    assert max(p.size) <= 1280
    assert max(t.size) <= 256
    assert p.format == "PNG" and t.format == "PNG"
