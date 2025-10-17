import h5py
import numpy as np

# Create simple contiguous dataset
with h5py.File('../simple_contiguous.h5', 'w') as f:
    # Create 1D array of 10 float64 values
    data = np.array([1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0], dtype=np.float64)
    
    # Create dataset with contiguous layout (no chunking)
    f.create_dataset('data', data=data, chunks=None)
    
print("Created simple_contiguous.h5 with 10 float64 values")
