package hdf5

import "github.com/scigolib/hdf5/internal/utils"

// ErrCorrupt is wrapped by errors that report invalid file metadata: a size
// that overflows, data that would lie past the end of the file, or a stored
// value the HDF5 format does not allow. Match it with errors.Is; the error
// message names the structure and its address.
var ErrCorrupt = utils.ErrCorrupt

// ErrLimit is wrapped by errors that refuse a read because it would allocate
// more than the file's read limit. The file itself may be valid; raise the
// limit to read it. Match it with errors.Is.
var ErrLimit = utils.ErrLimit
