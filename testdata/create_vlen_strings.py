#!/usr/bin/env python3
"""
Generate HDF5 test file with variable-length strings in compound datatype.
This tests Global Heap functionality.
"""

import h5py
import numpy as np

def create_vlen_string_test():
    """Create HDF5 file with compound datatype containing variable-length strings"""
    filename = 'vlen_strings.h5'

    # Create string datatype (variable-length)
    dt = h5py.special_dtype(vlen=str)

    # Create compound datatype with vlen string member
    compound_dtype = np.dtype([
        ('id', 'i4'),
        ('name', h5py.special_dtype(vlen=str)),
        ('value', 'f8')
    ])

    # Create test data
    data = np.array([
        (1, 'Alice', 3.14),
        (2, 'Bob', 2.71),
        (3, 'Charlie Brown', 1.41),
        (4, '', 0.0),  # Empty string
        (5, 'This is a very long string to test heap storage', 9.99)
    ], dtype=compound_dtype)

    # Create file and dataset
    with h5py.File(filename, 'w') as f:
        ds = f.create_dataset('compound_with_vlen', data=data)

        # Add attributes for verification
        f.attrs['description'] = 'Test file with variable-length strings in compound type'
        f.attrs['num_records'] = len(data)
        ds.attrs['field_count'] = 3

        print(f"Created {filename}")
        print(f"  Dataset: compound_with_vlen")
        print(f"  Records: {len(data)}")
        print(f"  Fields: id (int32), name (vlen string), value (float64)")
        print(f"\nSample data:")
        for i, row in enumerate(data):
            print(f"  [{i}] id={row['id']}, name='{row['name']}', value={row['value']}")

if __name__ == '__main__':
    create_vlen_string_test()
