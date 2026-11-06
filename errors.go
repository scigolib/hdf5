package hdf5

import (
	"errors"

	"github.com/scigolib/hdf5/internal/utils"
)

// ErrCorrupt is wrapped by errors that report invalid file metadata: a size
// that overflows, data that would lie past the end of the file, or a stored
// value the HDF5 format does not allow. Match it with errors.Is; the error
// message names the structure and its address.
var ErrCorrupt = utils.ErrCorrupt

// ErrLimit is wrapped by errors that refuse a read because it would allocate
// more than the file's read limit. The file itself may be valid; raise the
// limit to read it. Match it with errors.Is.
var ErrLimit = utils.ErrLimit

// ErrNotSupported is wrapped by errors that refuse valid HDF5 this library
// does not read or write yet: a filter it has no decoder for, a shared message
// stored in the shared-message heap, a datatype it cannot represent. The file
// is not corrupt; the C library may read it. Match it with errors.Is.
var ErrNotSupported = utils.ErrNotSupported

// ErrNeedsRepack is wrapped by the error OpenForWrite returns for a file it
// cannot make valid in place:
//   - version 2 object headers without a valid checksum (hdf5-go v0.13.8 and
//     earlier wrote none, v0.13.9 counted it in the chunk size), or a file
//     corrupted after it was written;
//   - integer or floating-point datatypes in the encoding of hdf5-go v0.13.9
//     and earlier;
//   - more legacy CRC32 checksums than the warning limit keeps, so not all of
//     them could be located for repair.
//
// The HDF5 C library cannot open such a file, and making it valid needs its
// object headers rewritten. Read it with Open (File.Warnings lists what was
// found) and write its contents into a new file. It is not ErrCorrupt: the
// file reads. Match it with errors.Is.
var ErrNeedsRepack = errors.New("hdf5: file needs to be rewritten into a new file")

// ErrNoData is wrapped by errors that refuse to read a dataset with no stored
// data and no defined fill value: its contiguous storage was never written and
// its fill value is undefined, so there is nothing to return (the C library
// fails the same read). The file is not corrupt. Match it with errors.Is.
var ErrNoData = utils.ErrNoData
