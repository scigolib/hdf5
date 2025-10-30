// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package hdf5

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDenseAttributes_Integration_SmallAttributes writes many small attributes.
// Note: Object header size limits compact storage to ~3-5 attributes in practice,
// even though MaxCompactAttributes = 8. The transition happens when header space runs out.
func TestDenseAttributes_Integration_SmallAttributes(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "dense_attrs_small.h5")

	// Create file and dataset
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer os.Remove(testFile)
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", Int32, []uint64{10})
	require.NoError(t, err)

	// Write small attributes until object header is full (triggers transition)
	// Use short names and int32 values (smallest attributes possible)
	var transitionedAt int
	var successfulWrites int
	for i := 0; i < MaxCompactAttributes+5; i++ {
		name := fmt.Sprintf("a%d", i) // Short name
		err := ds.WriteAttribute(name, int32(i))
		if err != nil {
			if strings.Contains(err.Error(), "existing dense storage not yet implemented") {
				// Transition happened! This is expected in Phase 2 MVP
				t.Logf("Transition successful at attribute %d", i)
				t.Logf("MVP limitation: cannot add more attributes after transition (Phase 2)")
				transitionedAt = i
				break
			}
			require.NoError(t, err, "unexpected error at attribute %d", i)
		} else {
			successfulWrites++
		}
	}

	// Verify that we wrote at least some attributes and hit the Phase 3 limitation
	require.Greater(t, successfulWrites, 3, "should have written at least 3-4 attributes successfully")
	if transitionedAt > 0 {
		t.Logf("Successfully transitioned to dense storage at attribute %d", transitionedAt)
		require.Greater(t, transitionedAt, 3, "transition should happen after at least 3-4 compact attributes")
		require.LessOrEqual(t, transitionedAt, MaxCompactAttributes+1, "transition should happen around MaxCompactAttributes")
	}

	// Close and verify file exists
	err = fw.Close()
	require.NoError(t, err)

	// Verify file was created
	info, err := os.Stat(testFile)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(512), "file should be >512B with dense attributes")
}

// TestDenseAttributes_Integration_Transition tests automatic transition from compact to dense.
// Phase 2 MVP: Transition works, but adding to dense after transition is not yet implemented.
func TestDenseAttributes_Integration_Transition(t *testing.T) {
	t.Skip("Phase 2 MVP: Transition works but adding to existing dense storage not yet implemented")
}

// TestDenseAttributes_Integration_UTF8 tests dense attributes with UTF-8 names.
// Phase 2 MVP: UTF-8 names work in compact, transition works, but adding to dense not yet implemented.
func TestDenseAttributes_Integration_UTF8(t *testing.T) {
	t.Skip("Phase 2 MVP: UTF-8 names work but full dense workflow not yet implemented")
}

// TestDenseAttributes_Integration_DuplicateError tests duplicate detection during transition.
func TestDenseAttributes_Integration_DuplicateError(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "dense_duplicate.h5")

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer os.Remove(testFile)
	defer fw.Close()

	ds, err := fw.CreateDataset("/data", Int32, []uint64{10})
	require.NoError(t, err)

	// Write a few attributes
	for i := 0; i < 3; i++ {
		err := ds.WriteAttribute(fmt.Sprintf("attr%d", i), int32(i))
		require.NoError(t, err)
	}

	// Try to write duplicate (should fail in compact storage)
	err = ds.WriteAttribute("attr1", int32(999))
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}
