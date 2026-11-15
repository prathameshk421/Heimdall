import unittest
import urllib.request
import urllib.parse
import urllib.error
import threading
import http.server
import sys, os

try:
    from app import FeedbackHandler
except ImportError:
    sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))
    from app import FeedbackHandler

class TestWebFormValidation(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.HTTPServer(("127.0.0.1", 0), FeedbackHandler)
        cls.port = cls.server.server_port
        cls.thread = threading.Thread(target=cls.server.serve_forever)
        cls.thread.daemon = True
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()

    def test_invalid_email_refused(self):
        url = f"http://127.0.0.1:{self.port}/submit"
        data = urllib.parse.urlencode({"name": "Bob", "email": "not-an-email", "message": "Hello"}).encode("utf-8")
        req = urllib.request.Request(url, data=data, method="POST")
        try:
            with urllib.request.urlopen(req) as resp:
                self.assertEqual(resp.status, 400, "Server should reject invalid email with 400 Bad Request")
        except urllib.error.HTTPError as e:
            self.assertEqual(e.code, 400)

if __name__ == "__main__":
    unittest.main()
