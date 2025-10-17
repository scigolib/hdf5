#!/usr/bin/env python3
"""Create a simple 3D chunked dataset for testing N-dimensional chunking."""

import h5py
import numpy as np

# Create 3D dataset: 8x6x4
data = np.arange(8 * 6 * 4, dtype='int32').reshape(8, 6, 4)

with h5py.File('../test_3d_chunked.h5', 'w') as f:
    # Create chunked dataset with chunks of 3x3x2
    f.create_dataset(
        'data3d',
        data=data,
        chunks=(3, 3, 2),
        compression=None  # No compression for simpler testing
    )

    print(f"Created 3D dataset: shape={data.shape}, chunks=(3,3,2)")
    print(f"Total elements: {data.size}")
    print(f"First 10 elements: {data.flat[:10]}")
