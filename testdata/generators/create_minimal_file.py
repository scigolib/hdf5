# create_minimal_file.py
import h5py
import numpy as np

# Файл версии 0 (HDF5 1.0)
filename = 'testdata/v0.h5'
with h5py.File(filename, 'w', libver='earliest') as f:
    f.create_dataset('test', data=[1, 2, 3])
print(f'Created HDF5 v0 file: {filename}')

# Файл версии 2 (HDF5 1.8)
filename = 'testdata/v2.h5'
with h5py.File(filename, 'w', libver='v108') as f:
    f.create_dataset('data', data=np.arange(10))
print(f'Created HDF5 v2 file: {filename}')

# Файл версии 3 (HDF5 1.10+)
filename = 'testdata/v3.h5'
with h5py.File(filename, 'w', libver='latest') as f:
    f.create_dataset('data', data=np.arange(10))
print(f'Created HDF5 v3 file: {filename}')

# Дополнительный файл с группами
filename = 'testdata/with_groups.h5'
with h5py.File(filename, 'w', libver='v108') as f:
    f.create_dataset('dataset1', data=[1.1, 2.2, 3.3])
    grp = f.create_group('subgroup')
    grp.create_dataset('dataset2', data=[4, 5, 6])
print(f'Created HDF5 with groups: {filename}')