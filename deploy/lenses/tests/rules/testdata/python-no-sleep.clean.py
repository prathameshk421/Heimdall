import threading


def test_refund():
    arrived = threading.Event()
    assert arrived.wait(timeout=1)
