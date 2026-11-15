import unittest
import urllib.request
import urllib.parse
import threading
import http.server
import sys, os

try:
    from app import FeedbackHandler
except ImportError:
    sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))
    from app import FeedbackHandler

class TestWebForm(unittest.TestCase):
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

    def test_get_form_page(self):
        url = f"http://127.0.0.1:{self.port}/"
        with urllib.request.urlopen(url) as resp:
            self.assertEqual(resp.status, 200)
            content = resp.read().decode("utf-8")
            self.assertIn("Customer Feedback", content)
            self.assertIn('id="feedback-form"', content)
            self.assertIn('name="email"', content)

    def test_valid_submission(self):
        url = f"http://127.0.0.1:{self.port}/submit"
        data = urllib.parse.urlencode({"name": "Alice", "email": "alice@example.com", "message": "Great service!"}).encode("utf-8")
        req = urllib.request.Request(url, data=data, method="POST")
        with urllib.request.urlopen(req) as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(resp.read().decode("utf-8"), "Feedback received")

if __name__ == "__main__":
    unittest.main()
