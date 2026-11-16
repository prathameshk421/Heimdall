# ruleid: python-seam-only
from app.internal.payments import refund


def test_refund():
    assert refund() == 100
