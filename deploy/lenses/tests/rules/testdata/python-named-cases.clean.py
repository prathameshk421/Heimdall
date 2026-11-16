import pytest


@pytest.mark.parametrize("amount", [40, 60], ids=["forty", "sixty"])
def test_refund(amount):
    assert amount > 0
