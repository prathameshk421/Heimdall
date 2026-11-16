import http.server
import urllib.parse
import os

HTML_PATH = os.path.join(os.path.dirname(__file__), "index.html")

class FeedbackHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/" or self.path == "/index.html":
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()
            with open(HTML_PATH, "rb") as f:
                self.wfile.write(f.read())
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        if self.path == "/submit":
            length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(length).decode("utf-8")
            params = urllib.parse.parse_qs(body)
            # Base implementation: does not validate email format or empty fields
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"Feedback received")
        else:
            self.send_response(404)
            self.end_headers()

def run_server(port=8080):
    server = http.server.HTTPServer(("127.0.0.1", port), FeedbackHandler)
    server.serve_forever()

if __name__ == "__main__":
    run_server()
