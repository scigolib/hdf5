#!/usr/bin/env python3
"""
Create HDF5 file with string datasets for testing
"""
import h5py
import numpy as np

def create_string_test():
    """Create test file with various string datasets"""
    with h5py.File('../string_test.h5', 'w') as f:
        # Fixed-length string dataset (null-terminated)
        # H5T_STR_NULLTERM = null-terminated
        dt_fixed = h5py.string_dtype(encoding='ascii', length=10)
        ds_fixed = f.create_dataset('fixed_strings', (3,), dtype=dt_fixed)
        ds_fixed[0] = 'hello'
        ds_fixed[1] = 'world'
        ds_fixed[2] = 'test'

        # Fixed-length string array (2D)
        ds_2d = f.create_dataset('string_matrix', (2, 3), dtype=dt_fixed)
        ds_2d[0, 0] = 'A1'
        ds_2d[0, 1] = 'A2'
        ds_2d[0, 2] = 'A3'
        ds_2d[1, 0] = 'B1'
        ds_2d[1, 1] = 'B2'
        ds_2d[1, 2] = 'B3'

        # Longer strings
        dt_long = h5py.string_dtype(encoding='ascii', length=50)
        ds_long = f.create_dataset('long_strings', (2,), dtype=dt_long)
        ds_long[0] = 'This is a longer test string'
        ds_long[1] = 'Another long string with more characters'

        print("Created string_test.h5 with:")
        print("  - fixed_strings: 3 strings of max length 10")
        print("  - string_matrix: 2x3 string array")
        print("  - long_strings: 2 strings of max length 50")

if __name__ == '__main__':
    create_string_test()
    print("\nDone! File created at ../string_test.h5")
