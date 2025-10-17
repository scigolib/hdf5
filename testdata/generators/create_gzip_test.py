#!/usr/bin/env python3
"""
Create HDF5 file with GZIP compressed dataset
"""
import h5py
import numpy as np

def create_gzip_test():
    """Create test file with GZIP compressed datasets"""
    with h5py.File('../gzip_test.h5', 'w') as f:
        # Create a simple 1D dataset with compression
        data_1d = np.arange(100, dtype='float64')
        ds1 = f.create_dataset('compressed_1d',
                               data=data_1d,
                               compression='gzip',
                               compression_opts=6)  # compression level

        print("Created compressed_1d: 100 elements")

        # Create a 2D dataset with compression
        data_2d = np.arange(600).reshape(20, 30).astype('float64')
        ds2 = f.create_dataset('compressed_2d',
                               data=data_2d,
                               compression='gzip',
                               compression_opts=6,
                               chunks=(5, 10))  # explicit chunking

        print("Created compressed_2d: 20x30 array, chunks=(5, 10)")

        # Create a dataset with shuffle filter + gzip (better compression)
        data_shuffled = np.random.rand(1000).astype('float32')
        ds3 = f.create_dataset('shuffled_compressed',
                               data=data_shuffled,
                               compression='gzip',
                               compression_opts=9,
                               shuffle=True,  # Enable shuffle filter
                               chunks=(100,))

        print("Created shuffled_compressed: 1000 elements, shuffle+gzip")

        # Create uncompressed dataset for comparison
        data_plain = np.arange(50, dtype='int32')
        ds4 = f.create_dataset('uncompressed',
                               data=data_plain,
                               compression=None)

        print("Created uncompressed: 50 elements (no compression)")

        print("\nAll datasets created successfully!")
        print(f"\nFile: ../gzip_test.h5")

        # Print compression info
        for name, ds in f.items():
            compression = ds.compression
            shuffle = ds.shuffle
            print(f"\n{name}:")
            print(f"  Shape: {ds.shape}")
            print(f"  Dtype: {ds.dtype}")
            print(f"  Compression: {compression}")
            print(f"  Shuffle: {shuffle}")
            if compression:
                print(f"  Compression opts: {ds.compression_opts}")

if __name__ == '__main__':
    create_gzip_test()
    print("\nDone!")
