#!/usr/bin/env python3
"""
Create test HDF5 files for Mathcad support testing
"""

import h5py
import numpy as np
import os

# Ensure parent directory exists
os.chdir(os.path.dirname(__file__) or '.')
os.chdir('..')  # Move to testdata/

print("Creating Mathcad test files...")

# 1. Simple float64 array
print("Creating simple_float64.h5...")
with h5py.File('simple_float64.h5', 'w', libver='latest') as f:
    f.create_dataset('data', data=np.array([1.0, 2.0, 3.0, 4.0, 5.0], dtype=np.float64))
print("  [OK] Created: simple 1D array of 5 float64 values")

# 2. 2D matrix
print("\nCreating matrix_2x3.h5...")
with h5py.File('matrix_2x3.h5', 'w', libver='latest') as f:
    matrix = np.array([[1.0, 2.0, 3.0],
                       [4.0, 5.0, 6.0]], dtype=np.float64)
    f.create_dataset('matrix', data=matrix)
print("  [OK] Created: 2x3 matrix of float64")

# 3. Multiple datasets
print("\nCreating multiple_datasets.h5...")
with h5py.File('multiple_datasets.h5', 'w', libver='latest') as f:
    f.create_dataset('vector_x', data=np.array([1.0, 2.0, 3.0], dtype=np.float64))
    f.create_dataset('vector_y', data=np.array([4.0, 5.0, 6.0], dtype=np.float64))
    f.create_dataset('scalar_c', data=np.array([42.0], dtype=np.float64))
print("  [OK] Created: 3 datasets (2 vectors + 1 scalar)")

# 4. With attributes
print("\nCreating with_attributes.h5...")
with h5py.File('with_attributes.h5', 'w', libver='latest') as f:
    # Create dataset with attributes
    ds = f.create_dataset('temperature', data=np.array([20.5, 21.0, 22.3], dtype=np.float64))
    ds.attrs['units'] = 'Celsius'
    ds.attrs['sensor'] = 'TMP36'
    ds.attrs['version'] = 1
print("  [OK] Created: dataset with 3 attributes")

# 5. Grouped structure (like Mathcad document)
print("\nCreating mathcad_document.h5...")
with h5py.File('mathcad_document.h5', 'w', libver='latest') as f:
    # Metadata group
    metadata = f.create_group('metadata')
    metadata.attrs['version'] = '1.0'
    metadata.attrs['author'] = 'Test User'

    # Variables group
    variables = f.create_group('variables')
    variables.create_dataset('matrix_A', data=np.array([[1, 2], [3, 4]], dtype=np.float64))
    variables.create_dataset('vector_x', data=np.array([10, 20, 30], dtype=np.float64))
    variables.create_dataset('scalar_c', data=np.array([3.14159], dtype=np.float64))

    # Calculations group
    calc = f.create_group('calculations')
    calc.create_dataset('result_1', data=np.array([100.0, 200.0], dtype=np.float64))
print("  [OK] Created: structured document with groups")

# 6. Different datatypes
print("\nCreating various_types.h5...")
with h5py.File('various_types.h5', 'w', libver='latest') as f:
    f.create_dataset('float64', data=np.array([1.0, 2.0, 3.0], dtype=np.float64))
    f.create_dataset('float32', data=np.array([1.0, 2.0, 3.0], dtype=np.float32))
    f.create_dataset('int32', data=np.array([1, 2, 3], dtype=np.int32))
    f.create_dataset('int64', data=np.array([1, 2, 3], dtype=np.int64))
print("  [OK] Created: datasets with different datatypes")

print("\n" + "="*50)
print("All test files created successfully!")
print("="*50)

# Print file info
print("\nTest files summary:")
for filename in ['simple_float64.h5', 'matrix_2x3.h5', 'multiple_datasets.h5',
                 'with_attributes.h5', 'mathcad_document.h5', 'various_types.h5']:
    size = os.path.getsize(filename)
    print(f"  {filename:30s} - {size:6d} bytes")
