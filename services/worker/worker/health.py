import logging
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Callable

logger = logging.getLogger(__name__)


def make_handler(is_ready: Callable[[], bool]):
    class _Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):  # suppress default access log
            pass

        def do_GET(self):
            if self.path == "/healthz":
                self._respond(200, b"ok")
            elif self.path == "/readyz":
                if is_ready():
                    self._respond(200, b"ok")
                else:
                    self._respond(503, b"not ready")
            else:
                self._respond(404, b"not found")

        def _respond(self, code: int, body: bytes):
            self.send_response(code)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    return _Handler


def make_readyz(
    rabbit_is_open: Callable[[], bool],
    ping_pg: Callable[[], None],
    ping_redis: Callable[[], None],
    ping_minio: Callable[[], None] | None = None,
) -> Callable[[], bool]:
    """Return a readiness callable that checks all dependency health."""

    def _is_ready() -> bool:
        try:
            if not rabbit_is_open():
                return False
            ping_pg()
            ping_redis()
            if ping_minio is not None:
                ping_minio()
            return True
        except Exception:
            logger.debug("Readiness check failed", exc_info=True)
            return False

    return _is_ready


def start_health_server(port: int, is_ready: Callable[[], bool]) -> HTTPServer:
    """Start a health/readiness probe server in a daemon thread."""
    server = HTTPServer(("", port), make_handler(is_ready))
    t = threading.Thread(target=server.serve_forever, daemon=True, name="health-server")
    t.start()
    logger.info("Health server started on port %d", port)
    return server
