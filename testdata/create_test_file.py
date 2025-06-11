import h5py
import numpy as np

# Используем совместимый формат HDF5 1.8
with h5py.File('testdata/simple.h5', 'w', libver='v108') as f:
    f.attrs['description'] = 'Simple test file'

    # Create datasets
    f.create_dataset('ints', data=np.array([1, 2, 3]), dtype='int32')
    f.create_dataset('floats', data=np.array([1.1, 2.2, 3.3]), dtype='float32')

    # Create groups
    grp1 = f.create_group('group1')
    grp1.create_dataset('data', data=np.arange(10), dtype='int32')

    grp2 = f.create_group('group2')
    grp2.create_dataset('matrix', data=np.eye(3), dtype='float32')