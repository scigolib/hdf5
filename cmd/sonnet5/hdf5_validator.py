#!/usr/bin/env python3
"""
HDF5 File Validator - Check if files are valid HDF5 format
"""

import h5py
import sys
import os

def validate_hdf5_file(filepath):
    """Validate an HDF5 file and print its structure"""
    print(f"\n=== Validating {filepath} ===")
    
    if not os.path.exists(filepath):
        print(f"❌ File does not exist: {filepath}")
        return False
        
    try:
        with h5py.File(filepath, 'r') as f:
            print(f"✅ File is valid HDF5")
            print(f"📄 File format version: {f.libver}")
            
            def print_structure(name, obj):
                if isinstance(obj, h5py.Dataset):
                    print(f"  📊 Dataset: {name} - Shape: {obj.shape}, Type: {obj.dtype}")
                elif isinstance(obj, h5py.Group):
                    print(f"  📁 Group: {name}")
            
            print("📋 File structure:")
            f.visititems(print_structure)
            
            return True
            
    except Exception as e:
        print(f"❌ Invalid HDF5 file: {e}")
        return False

def create_reference_files():
    """Create known-good reference HDF5 files for testing"""
    import numpy as np
    
    print("Creating reference HDF5 files...")
    
    # Simple v1.8 compatible file
    with h5py.File('reference_simple.h5', 'w', libver='earliest') as f:
        f.create_dataset('simple_data', data=np.array([1, 2, 3, 4, 5]))
    
    # File with groups
    with h5py.File('reference_groups.h5', 'w', libver='earliest') as f:
        f.create_dataset('root_dataset', data=np.array([10, 20, 30]))
        
        grp = f.create_group('my_group')
        grp.create_dataset('group_data', data=np.array([100, 200, 300]))
        
        nested = grp.create_group('nested')
        nested.create_dataset('nested_data', data=np.array([1000, 2000]))
    
    print("✅ Created reference_simple.h5")
    print("✅ Created reference_groups.h5")

def main():
    files_to_check = [
        'testdata/v0.h5',
        'testdata/v2.h5', 
        'testdata/v3.h5',
        'testdata/with_groups.h5'
    ]
    
    print("HDF5 File Validator")
    print("=" * 50)
    
    # Check if any files are valid
    valid_count = 0
    for filepath in files_to_check:
        if validate_hdf5_file(filepath):
            valid_count += 1
    
    print(f"\n📊 Summary: {valid_count}/{len(files_to_check)} files are valid HDF5")
    
    if valid_count == 0:
        print("\n🔧 All files are invalid. Creating reference files for comparison...")
        create_reference_files()
        print("\nTry testing your Go parser with the reference files:")
        print("  - reference_simple.h5")
        print("  - reference_groups.h5")

if __name__ == "__main__":
    main()
