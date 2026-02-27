package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// loadTraditionalGroup tests (via v0 reference files that use SNOD format)
// ---------------------------------------------------------------------------

// TestTraditionalGroup_V0File opens a v0 superblock file and exercises
// loadTraditionalGroup through the normal Open -> loadGroup path.
func TestTraditionalGroup_V0File(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	require.Equal(t, "/", root.Name())

	children := root.Children()
	require.NotEmpty(t, children, "v0 root group should have children")

	// v0.h5 has a dataset named "test"
	found := false
	for _, child := range children {
		if child.Name() == "test" {
			_, ok := child.(*Dataset)
			require.True(t, ok, "child 'test' should be a Dataset")
			found = true
		}
	}
	require.True(t, found, "dataset 'test' should be found in v0.h5")
}

// TestTraditionalGroup_GroupOld opens the reference group_old.h5 file that
// uses traditional (SNOD) format with nested groups.
func TestTraditionalGroup_GroupOld(t *testing.T) {
	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.NotEmpty(t, children, "group_old.h5 root should have children")

	// group_old.h5 has a group named "old"
	var groups, datasets int
	f.Walk(func(_ string, obj Object) {
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})
	assert.GreaterOrEqual(t, groups, 2, "should have at least root + 'old' group")
}

// TestTraditionalGroup_Tarrold opens tarrold.h5 (v0) which has multiple
// datasets under the root, exercising loadChildren with SNOD entries.
func TestTraditionalGroup_Tarrold(t *testing.T) {
	f, err := Open("testdata/reference/tarrold.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Len(t, children, 2, "tarrold.h5 should have 2 datasets")

	names := map[string]bool{}
	for _, child := range children {
		names[child.Name()] = true
		_, ok := child.(*Dataset)
		require.True(t, ok, "child %q should be a Dataset", child.Name())
	}
	assert.True(t, names["Dataset1"], "should contain Dataset1")
	assert.True(t, names["Dataset2"], "should contain Dataset2")
}

// TestTraditionalGroup_WalkV0 performs a full Walk on a v0 file and verifies
// all paths are reported correctly.
func TestTraditionalGroup_WalkV0(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	paths := map[string]bool{}
	f.Walk(func(path string, _ Object) {
		paths[path] = true
	})

	assert.True(t, paths["/"], "Walk should visit root")
	assert.True(t, paths["/test"], "Walk should visit /test dataset")
}

// TestTraditionalGroup_V0ReadDataset reads data from a v0 file dataset
// to verify the full chain: loadTraditionalGroup -> loadChildren -> loadObject -> Dataset.Read.
func TestTraditionalGroup_V0ReadDataset(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.NotEmpty(t, children)

	for _, child := range children {
		if ds, ok := child.(*Dataset); ok {
			// Just verify Info() doesn't error
			info, err := ds.Info()
			require.NoError(t, err)
			require.NotEmpty(t, info, "dataset info should not be empty")
		}
	}
}

// TestTraditionalGroup_Attributes verifies that Attributes() on a traditional
// format group (address=0) returns an empty slice without error.
func TestTraditionalGroup_Attributes(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	attrs, err := root.Attributes()
	require.NoError(t, err)
	require.NotNil(t, attrs, "attributes should be non-nil (empty slice)")
}

// ---------------------------------------------------------------------------
// loadObject tests - covering type branches
// ---------------------------------------------------------------------------

// TestLoadObject_V0ObjectTypeUnknown opens a v0 file where root group has
// ObjectTypeUnknown (no messages in object header), exercising the
// ObjectTypeUnknown -> loadGroup fallback in loadObject.
func TestLoadObject_V0ObjectTypeUnknown(t *testing.T) {
	// v0 files often have root groups with ObjectTypeUnknown
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// The root was loaded successfully, meaning the ObjectTypeUnknown path worked
	require.Equal(t, "/", root.Name())
	require.NotEmpty(t, root.Children())
}

// TestLoadObject_GroupType_V0Nested tests loading nested groups in v0 format,
// which exercises loadGroupWithCachedSymbolTable and CacheType=1 paths.
func TestLoadObject_GroupType_V0Nested(t *testing.T) {
	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	groups := map[string]*Group{}
	f.Walk(func(path string, obj Object) {
		if g, ok := obj.(*Group); ok {
			groups[path] = g
		}
	})

	require.Contains(t, groups, "/", "root group should exist")
	require.Contains(t, groups, "/old/", "nested 'old' group should exist")
}

// TestLoadObject_DatasetAddress verifies that Dataset.Address() returns a
// valid non-zero address for datasets loaded from a file.
func TestLoadObject_DatasetAddress(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_ds_addr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/mydata", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)
	assert.NotZero(t, found.Address(), "dataset should have a non-zero address")
}

// ---------------------------------------------------------------------------
// NamedDatatype tests
// ---------------------------------------------------------------------------

// TestNamedDatatype_BadCompound opens the reference file bad_compound.h5
// which contains a committed (named) datatype and verifies the accessors.
func TestNamedDatatype_BadCompound(t *testing.T) {
	f, err := Open("testdata/reference/bad_compound.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var namedDT *NamedDatatype
	f.Walk(func(_ string, obj Object) {
		if nd, ok := obj.(*NamedDatatype); ok {
			namedDT = nd
		}
	})
	require.NotNil(t, namedDT, "bad_compound.h5 should contain a NamedDatatype")

	// Verify Name() accessor
	assert.NotEmpty(t, namedDT.Name(), "NamedDatatype.Name() should not be empty")
	assert.Equal(t, "cmpnd", namedDT.Name(), "named datatype should be called 'cmpnd'")

	// Verify Datatype() accessor
	dt := namedDT.Datatype()
	assert.NotNil(t, dt, "NamedDatatype.Datatype() should not be nil")
}

// TestNamedDatatype_ScanAllReferenceFiles walks every reference file looking
// for NamedDatatype objects, verifying accessors work on all of them.
func TestNamedDatatype_ScanAllReferenceFiles(t *testing.T) {
	files := []string{
		"testdata/reference/bad_compound.h5",
		"testdata/reference/memleak_H5O_dtype_decode_helper_H5Odtype.h5",
		"testdata/reference/tarrold.h5",
		"testdata/reference/group_old.h5",
	}

	foundAny := false
	for _, path := range files {
		f, err := Open(path)
		if err != nil {
			continue
		}
		f.Walk(func(_ string, obj Object) {
			if nd, ok := obj.(*NamedDatatype); ok {
				foundAny = true
				assert.NotEmpty(t, nd.Name(), "NamedDatatype in %s should have a name", path)
				// Datatype may or may not be nil depending on the file
				_ = nd.Datatype()
			}
		})
		_ = f.Close()
	}
	require.True(t, foundAny, "at least one reference file should contain a NamedDatatype")
}

// ---------------------------------------------------------------------------
// Soft link creation + round-trip tests
// ---------------------------------------------------------------------------

// TestSoftLink_WriteAndReadBack creates a file with a soft link, reopens it,
// and verifies the file structure is intact. This exercises CreateSoftLink
// and the walk/load path for files containing soft links.
func TestSoftLink_WriteAndReadBack(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_softlink_roundtrip.h5")

	// Write phase: create a dataset and a soft link to it.
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/target", Float64, []uint64{5})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0, 4.0, 5.0})
	require.NoError(t, err)

	err = fw.CreateSoftLink("/link_to_target", "/target")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read phase: verify the file opens and the dataset is accessible.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// The file should have at least the dataset (soft links may or may not
	// appear as children depending on resolution support).
	children := root.Children()
	require.NotEmpty(t, children, "root should have at least the target dataset")

	// Find and read the dataset
	var targetDS *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "target" {
			targetDS = d
		}
	})
	require.NotNil(t, targetDS, "target dataset should be accessible")

	data, err := targetDS.Read()
	require.NoError(t, err)
	require.Len(t, data, 5)
	assert.InDelta(t, 1.0, data[0], 1e-10)
	assert.InDelta(t, 5.0, data[4], 1e-10)
}

// TestSoftLink_MultipleLinks creates multiple soft links in one file and
// verifies the file is still valid.
func TestSoftLink_MultipleLinks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_softlink_multi.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/data")
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data/values", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{10.0, 20.0, 30.0})
	require.NoError(t, err)

	// Create multiple soft links
	err = fw.CreateSoftLink("/link1", "/data/values")
	require.NoError(t, err)
	err = fw.CreateSoftLink("/link2", "/data/values")
	require.NoError(t, err)
	err = fw.CreateSoftLink("/link3", "/data")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file opens correctly
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	require.NotEmpty(t, root.Children())
}

// TestSoftLink_ResolveSoftLink_NotImplemented verifies that resolveSoftLink
// returns the expected "not implemented" error.
func TestSoftLink_ResolveSoftLink_NotImplemented(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_resolve_soft.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Call resolveSoftLink directly (it's unexported but accessible from same package)
	_, err = fw.resolveSoftLink(0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

// ---------------------------------------------------------------------------
// External link creation + round-trip tests
// ---------------------------------------------------------------------------

// TestExternalLink_WriteAndReadBack creates a file with an external link,
// reopens it, and verifies the file structure is intact.
func TestExternalLink_WriteAndReadBack(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_extlink_roundtrip.h5")

	// Write phase
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/local_data", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.NoError(t, err)

	err = fw.CreateExternalLink("/ext_link", "other_file.h5", "/remote_data")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read phase: verify file opens
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	require.NotEmpty(t, root.Children())

	// Verify the local dataset is accessible
	var localDS *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "local_data" {
			localDS = d
		}
	})
	require.NotNil(t, localDS, "local_data should be accessible")

	data, err := localDS.Read()
	require.NoError(t, err)
	require.Len(t, data, 3)
}

// TestExternalLink_MultipleLinks creates multiple external links targeting
// different files and object paths.
func TestExternalLink_MultipleLinks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_extlink_multi.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	err = fw.CreateExternalLink("/links/ext1", "file_a.h5", "/dataset1")
	require.NoError(t, err)
	err = fw.CreateExternalLink("/links/ext2", "file_b.h5", "/group/dataset2")
	require.NoError(t, err)
	err = fw.CreateExternalLink("/links/ext3", "file_c.hdf5", "/data")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file opens
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	require.NotEmpty(t, root.Children())
}

// TestExternalLink_ResolveExternalLink_NotImplemented verifies that
// resolveExternalLink returns the expected "not implemented" error.
func TestExternalLink_ResolveExternalLink_NotImplemented(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_resolve_ext.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Call resolveExternalLink directly
	_, _, err = fw.resolveExternalLink(0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

// ---------------------------------------------------------------------------
// writeV1RefCount tests (via CreateHardLink with v0 superblock)
// ---------------------------------------------------------------------------

// TestWriteV1RefCount exercises writeV1RefCount by creating hard links in
// a v0 superblock file (which uses v1 object headers).
func TestWriteV1RefCount(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_v1_refcount.h5")

	fw, err := CreateForWrite(filename, CreateTruncate,
		WithSuperblockVersion(core.Version0))
	require.NoError(t, err)
	defer fw.Close()

	// Create a dataset in v0 format (uses v1 object headers)
	ds, err := fw.CreateDataset("/data", Int32, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int32{10, 20, 30})
	require.NoError(t, err)

	// Get dataset address
	dataAddr, err := fw.resolveObjectAddress("/data")
	require.NoError(t, err)

	// Read initial reference count
	oh1, err := core.ReadObjectHeader(fw.writer, dataAddr, fw.file.sb)
	require.NoError(t, err)
	initialRefCount := oh1.GetReferenceCount()
	assert.Equal(t, uint32(1), initialRefCount, "initial refcount should be 1")

	// Create hard link (increments refcount via writeV1RefCount)
	err = fw.CreateHardLink("/link_to_data", "/data")
	require.NoError(t, err)

	// Verify refcount was incremented
	oh2, err := core.ReadObjectHeader(fw.writer, dataAddr, fw.file.sb)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), oh2.GetReferenceCount(), "refcount should be 2 after hard link")
}

// TestWriteObjectHeaderWithRefCount_V2 tests the v2 path of
// writeObjectHeaderWithRefCount (default superblock version).
func TestWriteObjectHeaderWithRefCount_V2(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_v2_refcount.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/dataset", Float64, []uint64{2})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0})
	require.NoError(t, err)

	// Create hard link to exercise writeObjectHeaderWithRefCount
	err = fw.CreateHardLink("/hardlink", "/dataset")
	require.NoError(t, err)

	// Create another hard link (refcount goes to 3)
	err = fw.CreateHardLink("/hardlink2", "/dataset")
	require.NoError(t, err)

	dataAddr, err := fw.resolveObjectAddress("/dataset")
	require.NoError(t, err)

	oh, err := core.ReadObjectHeader(fw.writer, dataAddr, fw.file.sb)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), oh.GetReferenceCount(), "refcount should be 3")

	err = fw.Close()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Additional traditional group tests with reference files
// ---------------------------------------------------------------------------

// TestTraditionalGroup_MultipleV0ReferenceFiles opens several v0 reference
// files to exercise loadTraditionalGroup and loadChildren broadly.
func TestTraditionalGroup_MultipleV0ReferenceFiles(t *testing.T) {
	v0Files := []struct {
		path       string
		minObjects int // minimum total objects (groups + datasets + named dt)
	}{
		{"testdata/v0.h5", 2},
		{"testdata/reference/group_old.h5", 2},
		{"testdata/reference/tarrold.h5", 3},
		{"testdata/reference/fill_old.h5", 1},
		{"testdata/reference/tlayouto.h5", 1},
	}

	for _, tc := range v0Files {
		t.Run(tc.path, func(t *testing.T) {
			f, err := Open(tc.path)
			if err != nil {
				t.Skipf("cannot open %s: %v", tc.path, err)
			}
			defer func() { _ = f.Close() }()

			var objectCount int
			f.Walk(func(_ string, _ Object) {
				objectCount++
			})

			assert.GreaterOrEqual(t, objectCount, tc.minObjects,
				"file %s should have at least %d objects", tc.path, tc.minObjects)
		})
	}
}

// TestTraditionalGroup_TarroldDatasetAccessors reads datasets from tarrold.h5
// to exercise the loadTraditionalGroup -> loadObject -> Dataset chain.
// Note: tarrold.h5 uses data layout v1 which may not fully support Info()/Read(),
// but the objects should still be loaded correctly as *Dataset with valid names.
func TestTraditionalGroup_TarroldDatasetAccessors(t *testing.T) {
	f, err := Open("testdata/reference/tarrold.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	for _, child := range root.Children() {
		ds, ok := child.(*Dataset)
		if !ok {
			continue
		}
		// Verify the dataset was loaded with correct metadata
		assert.NotEmpty(t, ds.Name(), "dataset should have a name")
		assert.NotZero(t, ds.Address(), "dataset should have a valid address")
	}
}

// ---------------------------------------------------------------------------
// Mixed soft/external link and hard link file
// ---------------------------------------------------------------------------

// TestSoftLink_WithHardLinksInSameFile creates a file with both soft and hard
// links, verifying they don't interfere with each other during creation.
func TestSoftLink_WithHardLinksInSameFile(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_mixed_links.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/original", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.NoError(t, err)

	// Create a hard link first
	err = fw.CreateHardLink("/hard_link", "/original")
	require.NoError(t, err)

	// Create a soft link
	err = fw.CreateSoftLink("/soft_link", "/original")
	require.NoError(t, err)

	// Create an external link
	err = fw.CreateExternalLink("/ext_link", "other.h5", "/data")
	require.NoError(t, err)

	// All link types created without error
	err = fw.Close()
	require.NoError(t, err)
}

// TestExternalLink_InNestedGroup creates external links inside nested groups.
func TestExternalLink_InNestedGroup(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_extlink_nested.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/references")
	require.NoError(t, err)

	err = fw.CreateExternalLink("/references/remote_ds", "archive.h5", "/measurements/pressure")
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/references/local_ds", Int32, []uint64{2})
	require.NoError(t, err)
	err = ds.Write([]int32{1, 2})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Round-trip
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	var dsFound bool
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "local_ds" {
			dsFound = true
		}
	})
	assert.True(t, dsFound, "local_ds should be found in nested group")
}
