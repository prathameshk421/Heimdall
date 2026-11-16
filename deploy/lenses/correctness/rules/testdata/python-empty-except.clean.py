def read(path):
    try:
        return open(path).read()
    except OSError as error:
        raise RuntimeError("cannot read the file") from error
