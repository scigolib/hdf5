module github.com/scigolib/hdf5

go 1.25

require github.com/stretchr/testify v1.12.1

require go.yaml.in/yaml/v3 v3.0.5 // indirect

retract (
	// Wrote files no HDF5 tool can open (CRC32 instead of lookup3 checksums, v2 object headers without checksums); use v0.14.3+ to read them.
	[v0.9.0-beta, v0.13.9]
)
