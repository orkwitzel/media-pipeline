import logging
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

logger = logging.getLogger(__name__)


def make_handler(is_ready):
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


def start_health_server(port: int, is_ready) -> HTTPServer:
    """Start a health/readiness probe server in a daemon thread."""
    server = HTTPServer(("", port), make_handler(is_ready))
    t = threading.Thread(target=server.serve_forever, daemon=True, name="health-server")
    t.start()
    logger.info("Health server started on port %d", port)
    return server
