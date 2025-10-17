#include "hdf5.h"
#include <stdio.h>

int main() {
    hid_t file, space, dset;
    hsize_t dims[1] = {10};
    double data[10] = {1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0};
    
    file = H5Fcreate("test_contiguous.h5", H5F_ACC_TRUNC, H5P_DEFAULT, H5P_DEFAULT);
    space = H5Screate_simple(1, dims, NULL);
    dset = H5Dcreate(file, "data", H5T_IEEE_F64LE, space, 
                     H5P_DEFAULT, H5P_DEFAULT, H5P_DEFAULT);
    
    H5Dwrite(dset, H5T_NATIVE_DOUBLE, H5S_ALL, H5S_ALL, H5P_DEFAULT, data);
    
    H5Dclose(dset);
    H5Sclose(space);
    H5Fclose(file);
    
    printf("Created test_contiguous.h5\n");
    return 0;
}
