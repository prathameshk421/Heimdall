from app.payments import refund


def test_refund():
    assert refund() == 100
