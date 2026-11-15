import pytest


# ruleid: python-named-cases
@pytest.mark.parametrize("amount", [40, 60])
def test_refund(amount):
    assert amount > 0
