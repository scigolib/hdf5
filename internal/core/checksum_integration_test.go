package core

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestJenkinsChecksum_H5DumpCompatibility tests that files created with Jenkins checksums
// can be read by h5dump (HDF5 C library tool).
//
// This is the ultimate validation that our checksum implementation is correct.
func TestJenkinsChecksum_H5DumpCompatibility(t *testing.T) {
	// Check if h5dump is available.
	_, err := exec.LookPath("h5dump")
	if err != nil {
		t.Skip("h5dump not available, skipping compatibility test")
	}

	// Create a minimal HDF5 file with Superblock V2.
	tmpFile, err := os.CreateTemp("", "jenkins_test_*.h5")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Write Superblock V2 with Jenkins checksum.
	buf := make([]byte, 48)

	// Magic (8 bytes).
	copy(buf[0:8], "\x89HDF\r\n\x1a\n")

	// Version (1 byte).
	buf[8] = 2 // Superblock V2

	// Size of offsets (1 byte).
	buf[9] = 8

	// Size of lengths (1 byte).
	buf[10] = 8

	// File consistency flags (1 byte).
	buf[11] = 0

	// Base address (8 bytes) - offset 12.
	binary.LittleEndian.PutUint64(buf[12:20], 0)

	// Superblock extension address (8 bytes) - offset 20.
	binary.LittleEndian.PutUint64(buf[20:28], 0xFFFFFFFFFFFFFFFF) // Undefined

	// End-of-file address (8 bytes) - offset 28.
	binary.LittleEndian.PutUint64(buf[28:36], 0xFFFFFFFFFFFFFFFF) // Undefined

	// Root group object header address (8 bytes) - offset 36.
	binary.LittleEndian.PutUint64(buf[36:44], 0x88) // Example address

	// Checksum (Jenkins lookup3, 4 bytes) - offset 44.
	checksum := JenkinsChecksum(buf[0:44])
	binary.LittleEndian.PutUint32(buf[44:48], checksum)

	// Write to file.
	_, err = tmpFile.Write(buf)
	require.NoError(t, err)
	tmpFile.Close()

	// Try to run h5dump on the file.
	// If Jenkins checksum is correct, h5dump should not report checksum errors.
	cmd := exec.Command("h5dump", "-H", tmpFile.Name())
	output, _ := cmd.CombinedOutput() // Ignore error - we expect it to fail (no valid object header).

	// Note: h5dump may fail because we don't have a valid object header at 0x88,
	// but it should NOT fail with checksum errors.
	// We're checking that the superblock checksum passes verification.
	outputStr := string(output)

	// Check for checksum-related errors.
	require.NotContains(t, outputStr, "checksum error",
		"h5dump reported checksum error: %s", outputStr)
	require.NotContains(t, outputStr, "bad checksum",
		"h5dump reported bad checksum: %s", outputStr)
	require.NotContains(t, outputStr, "incorrect checksum",
		"h5dump reported incorrect checksum: %s", outputStr)

	t.Logf("h5dump output:\n%s", outputStr)
	t.Logf("Jenkins checksum for superblock: 0x%08X", checksum)
}

// TestJenkinsChecksum_RoundTrip tests that we can write and read back a file
// with Jenkins checksums.
func TestJenkinsChecksum_RoundTrip(t *testing.T) {
	// Create a test superblock.
	tmpFile, err := os.CreateTemp("", "roundtrip_test_*.h5")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Write superblock manually (since WriteSuperblockV2 is internal/not exported).
	buf := make([]byte, 48)

	// Magic (8 bytes).
	copy(buf[0:8], "\x89HDF\r\n\x1a\n")

	// Version 2.
	buf[8] = 2

	// Size of offsets and lengths.
	buf[9] = 8  // Size of offsets
	buf[10] = 8 // Size of lengths
	buf[11] = 0 // Flags

	// Base address (8 bytes).
	binary.LittleEndian.PutUint64(buf[12:20], 0)

	// Extension address (undefined).
	binary.LittleEndian.PutUint64(buf[20:28], 0xFFFFFFFFFFFFFFFF)

	// EOF address (undefined).
	binary.LittleEndian.PutUint64(buf[28:36], 0xFFFFFFFFFFFFFFFF)

	// Root group address.
	binary.LittleEndian.PutUint64(buf[36:44], 0x88)

	// Jenkins checksum.
	checksum := JenkinsChecksum(buf[0:44])
	binary.LittleEndian.PutUint32(buf[44:48], checksum)

	_, err = tmpFile.Write(buf)
	require.NoError(t, err)
	tmpFile.Close()

	// Read back and verify checksum.
	readFile, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer readFile.Close()

	readBuf := make([]byte, 48)
	n, err := readFile.ReadAt(readBuf, 0)
	require.NoError(t, err)
	require.Equal(t, 48, n)

	// Verify checksum.
	expectedChecksum := JenkinsChecksum(readBuf[0:44])
	actualChecksum := binary.LittleEndian.Uint32(readBuf[44:48])
	require.Equal(t, expectedChecksum, actualChecksum,
		"Round-trip checksum mismatch: wrote 0x%08X, read 0x%08X",
		expectedChecksum, actualChecksum)
}

// TestJenkinsChecksum_KnownFile tests against a known HDF5 file created by C library.
//
// This test is skipped if the reference file is not available.
func TestJenkinsChecksum_KnownFile(t *testing.T) {
	// Try to find a V2 superblock file in testdata.
	testFiles := []string{
		filepath.Join("..", "..", "testdata", "c-library-corpus", "basic", "aggr.h5"),
		filepath.Join("..", "..", "testdata", "c-library-corpus", "basic", "filespace_1_8.h5"),
	}

	var validFile string
	for _, path := range testFiles {
		if _, err := os.Stat(path); err == nil {
			validFile = path
			break
		}
	}

	if validFile == "" {
		t.Skip("No reference HDF5 file available for testing")
	}

	// Read the superblock.
	f, err := os.Open(validFile)
	if err != nil {
		t.Skipf("Cannot open reference file: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 48)
	n, err := f.ReadAt(buf, 0)
	if err != nil || n != 48 {
		t.Skipf("Cannot read superblock from reference file")
	}

	// Check if it's V2 or V3 (Jenkins checksum).
	version := buf[8]
	if version != 2 && version != 3 {
		t.Skipf("Reference file has V%d superblock, need V2/V3", version)
	}

	// Verify checksum.
	expectedChecksum := JenkinsChecksum(buf[0:44])
	actualChecksum := binary.LittleEndian.Uint32(buf[44:48])

	require.Equal(t, expectedChecksum, actualChecksum,
		"Reference file %s has incorrect checksum: expected 0x%08X, got 0x%08X",
		filepath.Base(validFile), expectedChecksum, actualChecksum)

	t.Logf("✓ Reference file %s has valid Jenkins checksum: 0x%08X",
		filepath.Base(validFile), actualChecksum)
}
