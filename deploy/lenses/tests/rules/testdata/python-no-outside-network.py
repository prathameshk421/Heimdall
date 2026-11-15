import requests


def test_refund():
    # ruleid: python-no-outside-network
    return requests.get("https://example.invalid/refunds")
