"""GET /healthz (KAN-22)."""


def handle(method, db):
    """Return (status, body). db is passed in so the endpoint can be shown never to use it (AC1)."""
    if method != "GET":
        return 405, "Method Not Allowed"
    return 200, "OK"
