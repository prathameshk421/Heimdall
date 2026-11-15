# ruleid: python-reaches-the-product
import os
import sys


def test_refund():
    assert os.environ["HEIMDALL_SEED"] == sys.argv[0]
