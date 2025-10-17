#!/usr/bin/env python3
"""
Create HDF5 test file with compound datatype
"""
import h5py
import numpy as np

# Define compound dtype - struct with multiple fields
compound_dtype = np.dtype([
    ('id', np.int32),
    ('temperature', np.float32),
    ('pressure', np.float64),
    ('name', 'S10')  # Fixed-length string, 10 bytes
])

# Create test data
data = np.array([
    (1, 20.5, 101.3, b'Sample A'),
    (2, 25.3, 100.8, b'Sample B'),
    (3, 30.1, 99.5, b'Sample C'),
    (4, 22.7, 102.1, b'Sample D'),
    (5, 28.9, 100.2, b'Sample E'),
], dtype=compound_dtype)

# Create HDF5 file
with h5py.File('testdata/compound_test.h5', 'w') as f:
    # Create dataset with compound type
    ds = f.create_dataset('measurements', data=data)

    # Create 2D dataset with compound type
    data_2d = np.array([
        [(1, 20.5, 101.3, b'A1'), (2, 25.3, 100.8, b'B1'), (3, 30.1, 99.5, b'C1')],
        [(4, 22.7, 102.1, b'A2'), (5, 28.9, 100.2, b'B2'), (6, 24.3, 103.5, b'C2')],
    ], dtype=compound_dtype)

    ds_2d = f.create_dataset('matrix', data=data_2d)

print("Created compound_test.h5 with:")
print(f"  - measurements: {data.shape} compound array")
print(f"  - matrix: {data_2d.shape} 2D compound array")
print(f"\nCompound type fields:")
print(f"  - id: int32")
print(f"  - temperature: float32")
print(f"  - pressure: float64")
print(f"  - name: S10 (fixed-length string)")
