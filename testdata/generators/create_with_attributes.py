#!/usr/bin/env python3
"""Create HDF5 file with various attributes for testing."""

import h5py
import numpy as np

with h5py.File('../with_attributes.h5', 'w') as f:
    # Create root group attributes
    f.attrs['title'] = 'Test File'
    f.attrs['version'] = 1
    f.attrs['pi'] = 3.14159
    f.attrs['array_attr'] = [1, 2, 3, 4, 5]

    # Create dataset with attributes
    data = np.arange(20, dtype='float64').reshape(4, 5)
    dset = f.create_dataset('dataset1', data=data)

    dset.attrs['units'] = 'meters'
    dset.attrs['scale'] = 1.5
    dset.attrs['offset'] = -10
    dset.attrs['valid_range'] = [0.0, 100.0]

    # Create group with attributes
    grp = f.create_group('group1')
    grp.attrs['description'] = 'Test Group'
    grp.attrs['count'] = 42

    print("Created file with attributes:")
    print(f"Root attributes: {list(f.attrs.keys())}")
    print(f"Dataset attributes: {list(dset.attrs.keys())}")
    print(f"Group attributes: {list(grp.attrs.keys())}")
