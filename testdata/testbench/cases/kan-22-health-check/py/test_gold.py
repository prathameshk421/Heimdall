import unittest

import health


class SpyDB:
    def __init__(self):
        self.pings = 0

    def ping(self):
        self.pings += 1


class HealthzTest(unittest.TestCase):
    def test_healthz_returns_ok(self):  # AC1
        db = SpyDB()
        status, body = health.handle("GET", db)
        self.assertEqual(status, 200)
        self.assertEqual(body, "OK")
        self.assertEqual(db.pings, 0)

    def test_healthz_refuses_other_methods(self):  # AC2
        for method in ["POST", "PUT", "DELETE", "PATCH", "HEAD"]:
            with self.subTest(method=method):
                status, _ = health.handle(method, SpyDB())
                self.assertEqual(status, 405)


if __name__ == "__main__":
    unittest.main()
