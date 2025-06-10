# testdata/create_test_file.py
import h5py
import numpy as np

# Используем надежный формат
with h5py.File('testdata/simple.h5', 'w') as f:
    f.attrs['description'] = 'Simple test file'

    # Создаем наборы данных с явным указанием типов
    f.create_dataset('ints', data=np.array([1, 2, 3], dtype='<i4'))
    f.create_dataset('floats', data=np.array([1.1, 2.2, 3.3], dtype='<f4'))

    # Создаем группы
    grp1 = f.create_group('group1')
    grp1.create_dataset('data', data=np.arange(10), dtype='<i4')

    grp2 = f.create_group('group2')
    grp2.create_dataset('matrix', data=np.eye(3), dtype='<f4')