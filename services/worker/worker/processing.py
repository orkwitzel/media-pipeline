import io
from PIL import Image, ImageDraw


def _resize(img: Image.Image, max_px: int) -> Image.Image:
    img = img.copy()
    img.thumbnail((max_px, max_px))
    return img


def process_image(data: bytes, *, thumb_px: int, max_px: int, watermark: str) -> tuple[bytes, bytes]:
    src = Image.open(io.BytesIO(data)).convert("RGB")
    processed = _resize(src, max_px)
    draw = ImageDraw.Draw(processed)
    draw.text((10, processed.size[1] - 20), watermark, fill=(255, 255, 255))
    thumb = _resize(src, thumb_px)
    def to_png(im: Image.Image) -> bytes:
        b = io.BytesIO(); im.save(b, "PNG"); return b.getvalue()
    return to_png(processed), to_png(thumb)
