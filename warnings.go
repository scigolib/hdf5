package hdf5

import (
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
)

// WarningCode classifies a Warning.
type WarningCode uint8

// Warning codes. Their values never change between releases.
const (
	// WarnLegacySuperblockChecksum: the version 2/3 superblock carries a
	// CRC32-IEEE checksum instead of lookup3, as hdf5-go v0.13.4 and earlier
	// wrote it. OpenForWrite repairs it.
	WarnLegacySuperblockChecksum = WarningCode(core.WarnLegacySuperblockChecksum)
	// WarnLegacyMetadataChecksum: a version 2 B-tree node, fractal heap header
	// or fractal heap indirect block carries CRC32-IEEE instead of lookup3, as
	// hdf5-go v0.13.4 and earlier wrote it. OpenForWrite repairs it.
	WarnLegacyMetadataChecksum = WarningCode(core.WarnLegacyMetadataChecksum)
	// WarnHeaderChecksum: a version 2 object header chunk has no valid
	// checksum: hdf5-go v0.13.8 and earlier wrote none, or the file is
	// corrupted. OpenForWrite refuses such a file with ErrNeedsRepack.
	WarnHeaderChecksum = WarningCode(core.WarnHeaderChecksum)
	// WarnHeaderChunkSize: a version 2 object header counts its checksum in
	// its chunk size, as hdf5-go v0.13.9 wrote it. OpenForWrite refuses such a
	// file with ErrNeedsRepack.
	WarnHeaderChunkSize = WarningCode(core.WarnHeaderChunkSize)
	// WarnLegacyDenseLayout: dense attribute storage in the layout hdf5-go
	// v0.14.2 and earlier wrote. The first change to the object's attributes
	// through OpenForWrite migrates it to the C layout.
	WarnLegacyDenseLayout = WarningCode(core.WarnLegacyDenseLayout)
	// WarnLegacyDatatype: an integer or floating-point datatype in the
	// encoding hdf5-go v0.13.9 and earlier wrote. It is read as the type the
	// writer meant; the C library cannot read it, and OpenForWrite refuses such
	// a file with ErrNeedsRepack.
	WarnLegacyDatatype = WarningCode(core.WarnLegacyDatatype)
	// WarnSuppressed: the warning limit was reached; the Message says how many
	// further warnings were dropped.
	WarnSuppressed = WarningCode(core.WarnSuppressed)
)

var warningCodeNames = map[WarningCode]string{
	WarnLegacySuperblockChecksum: "legacy superblock checksum",
	WarnLegacyMetadataChecksum:   "legacy metadata checksum",
	WarnHeaderChecksum:           "object header checksum",
	WarnHeaderChunkSize:          "object header chunk size",
	WarnLegacyDenseLayout:        "legacy dense storage layout",
	WarnLegacyDatatype:           "legacy datatype encoding",
	WarnSuppressed:               "warnings suppressed",
}

// String returns a short name of the code.
func (c WarningCode) String() string {
	if name, ok := warningCodeNames[c]; ok {
		return name
	}
	return fmt.Sprintf("WarningCode(%d)", uint8(c))
}

// Warning is a finding about a file that did not stop it from being read.
type Warning struct {
	Code WarningCode
	// Addr is the file address of the structure concerned, 0 for the file as a
	// whole (the superblock).
	Addr uint64
	// Message says what was found, which hdf5-go releases write it, whether
	// the HDF5 C library can open the file and how to fix it.
	Message string
}

// String returns the warning as "code at 0xADDR: message".
func (w Warning) String() string {
	return fmt.Sprintf("%s at 0x%X: %s", w.Code, w.Addr, w.Message)
}

// Warnings returns what the reader found so far that did not stop it but that
// the user of the file should know about: structures written by earlier
// hdf5-go releases in a form the HDF5 C library (h5dump, h5py, MATLAB, …)
// cannot open, or checksums that do not match. Such a file reads correctly
// here; each Message says how to make it readable by the C library.
//
// Warnings are collected as structures are read, so the list grows as more of
// the file is visited (open, then walk groups, read attributes, datasets).
// Each structure is reported once per code, and at most 1000 warnings are
// kept; a final WarnSuppressed entry counts the rest. A file written by the
// C library or by this release of hdf5-go has none.
//
// The returned slice is a copy; it is nil when there are no warnings.
func (f *File) Warnings() []Warning {
	src := f.sb.Warnings()
	if len(src) == 0 {
		return nil
	}
	out := make([]Warning, len(src))
	for i, w := range src {
		out[i] = Warning{Code: WarningCode(w.Code), Addr: w.Addr, Message: w.Message}
	}
	return out
}
