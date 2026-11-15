import unittest
try:
    from service import AccountService
except ImportError:
    import sys, os
    sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))
    from service import AccountService

class TestAccountOverdraft(unittest.TestCase):
    def setUp(self):
        self.svc = AccountService(":memory:")
        self.svc.create_account("acc1", 100)
        self.svc.create_account("acc2", 50)

    def test_overdraft_refused(self):
        with self.assertRaises(ValueError):
            self.svc.transfer("acc1", "acc2", 200)

    def test_negative_transfer_refused(self):
        with self.assertRaises(ValueError):
            self.svc.transfer("acc1", "acc2", -50)

if __name__ == "__main__":
    unittest.main()
