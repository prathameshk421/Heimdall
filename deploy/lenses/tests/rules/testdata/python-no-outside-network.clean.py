import requests


def test_refund():
    return requests.get("http://127.0.0.1:8080/refunds")
