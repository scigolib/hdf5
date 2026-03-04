# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "numpy",
#     "h5py"
# ]
# ///
import sys

import h5py
import numpy as np


def main(filename):
    with h5py.File(filename, "r") as h_file:
        group = h_file["group"]
        int_data = group["uint"]
        float_data = group["float"]

        for i in range(5):
            assert int_data[i] == i
            assert float_data[i] == i

main(sys.argv[1])
