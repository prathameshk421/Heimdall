def read(path):
    try:
        return open(path).read()
    # ruleid: python-empty-except
    except OSError:
        pass
