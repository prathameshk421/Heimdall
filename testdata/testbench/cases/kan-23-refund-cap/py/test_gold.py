import unittest

import refunds


def make_service():
    return refunds.RefundService({"p1": {"merchant_id": "m_42", "captured": 100}})


class RefundCapTest(unittest.TestCase):
    def test_over_cap_refused(self):  # AC1
        status, _ = make_service().refund("p1", 150)
        self.assertEqual(status, 400)

    def test_running_total_over_cap_refused(self):  # AC1
        svc = make_service()
        status, _ = svc.refund("p1", 40)
        self.assertEqual(status, 201)
        status, _ = svc.refund("p1", 70)
        self.assertEqual(status, 400)

    def test_running_total_exactly_at_cap_accepted(self):  # AC1
        svc = make_service()
        svc.refund("p1", 40)
        status, _ = svc.refund("p1", 60)
        self.assertEqual(status, 201)

    def test_response_names_merchant(self):  # AC2
        status, body = make_service().refund("p1", 40)
        self.assertEqual(status, 201)
        self.assertIn("merchant_id", body)
        self.assertEqual(body["merchant_id"], "m_42")


if __name__ == "__main__":
    unittest.main()
