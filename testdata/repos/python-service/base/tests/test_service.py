import unittest
try:
    from service import AccountService
except ImportError:
    import sys, os
    sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))
    from service import AccountService

class TestAccountService(unittest.TestCase):
    def setUp(self):
        self.svc = AccountService(":memory:")
        self.svc.create_account("acc1", 100)
        self.svc.create_account("acc2", 50)

    def test_initial_balance(self):
        self.assertEqual(self.svc.get_balance("acc1"), 100)
        self.assertEqual(self.svc.get_balance("acc2"), 50)

    def test_transfer_success(self):
        self.svc.transfer("acc1", "acc2", 30)
        self.assertEqual(self.svc.get_balance("acc1"), 70)
        self.assertEqual(self.svc.get_balance("acc2"), 80)

if __name__ == "__main__":
    unittest.main()
