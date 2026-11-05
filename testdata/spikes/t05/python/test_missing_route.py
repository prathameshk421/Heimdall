from urllib.error import HTTPError
from urllib.request import urlopen

from http.server import BaseHTTPRequestHandler, HTTPServer
from threading import Thread


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_error(404)

    def log_message(self, format, *args):
        pass


def test_missing_route_returns_success():
    server = HTTPServer(("127.0.0.1", 0), Handler)
    thread = Thread(target=server.serve_forever)
    thread.start()
    try:
        try:
            with urlopen(f"http://127.0.0.1:{server.server_port}/missing") as response:
                status_code = response.status
        except HTTPError as error:
            status_code = error.code
        assert status_code == 200, f"missing route status = {status_code}, want 200"
    finally:
        server.shutdown()
        thread.join()
        server.server_close()
