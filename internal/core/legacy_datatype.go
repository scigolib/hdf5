package core

import "bytes"

// legacyDatatypeMessage is the warning text for a datatype in the encoding of
// hdf5-go v0.13.9 and earlier; its arguments name the structure and address.
const legacyDatatypeMessage = "%s at 0x%X has a datatype in the encoding hdf5-go v0.13.9 and earlier wrote " +
	"(bit offset and precision as single bytes); it is read as the IEEE 754 or integer type meant, but the " +
	"HDF5 C library cannot read it (read the file with Open and rewrite it into a new file)"

// earlyIntegerFlags are the class bit field flags the early writer could set
// on an integer: byte order (bit 0) and sign (bit 3).
const earlyIntegerFlags = 0x09

// earlyHDF5GoNumericProps returns the property bytes the numeric datatype
// encoder of hdf5-go v0.11.0-beta to v0.13.9 wrote (encodeDatatypeNumeric
// before commit 2f92dd0): byte order, precision in bits and zeros for an
// integer; for an IEEE float additionally exponent size, mantissa size and a
// one-byte bias of 127 for both sizes. It returns nil for a class or size that
// encoder did not write.
func earlyHDF5GoNumericProps(class DatatypeClass, size uint32, byteOrder byte) []byte {
	switch class {
	case DatatypeFixed:
		if size != 1 && size != 2 && size != 4 && size != 8 {
			return nil
		}
		return []byte{byteOrder, byte(size * 8), 0, 0}
	case DatatypeFloat:
		var esize, msize byte
		switch size {
		case 4:
			esize, msize = 8, 23
		case 8:
			esize, msize = 11, 52
		default:
			return nil
		}
		return []byte{byteOrder, byte(size * 8), 0, esize, msize, 127, 0, 0, 0, 0, 0, 0}
	default:
		return nil
	}
}

// isEarlyHDF5GoNumeric reports the stored form of a numeric datatype written
// by hdf5-go v0.11.0-beta to v0.13.9: version 1, only the flags that encoder
// set, and exactly its property bytes. Read with the C layout, the first two
// bytes give a bit offset of at least 8*size*256, past the end of the type,
// which the C library rejects (H5Odtype.c:175-186 for integers, :235-241 and
// the offset check for floats), so no valid file contains it. Files our users
// wrote stay readable: the decoder reads it as the type the writer meant
// (documented deviation, ADR-004) and the reader reports WarnLegacyDatatype.
func isEarlyHDF5GoNumeric(class DatatypeClass, version uint8, flags, size uint32, props []byte) bool {
	if version != dtypeVersion1 {
		return false
	}
	allowed := uint32(flagBigEndian)
	if class == DatatypeFixed {
		allowed = earlyIntegerFlags
	}
	if flags&^allowed != 0 {
		return false
	}
	want := earlyHDF5GoNumericProps(class, size, byte(flags&flagBigEndian))
	return want != nil && bytes.Equal(props, want)
}

// IsEarlyHDF5GoDatatype reports whether dt, as returned by the decoder, was
// stored in the numeric encoding of hdf5-go v0.13.9 and earlier (see
// isEarlyHDF5GoNumeric). The decoder keeps the byte order flag and the
// stored property bytes, which identify the encoding after decoding.
func IsEarlyHDF5GoDatatype(dt *DatatypeMessage) bool {
	if dt == nil || dt.Version != dtypeVersion1 {
		return false
	}
	want := earlyHDF5GoNumericProps(dt.Class, dt.Size, byte(dt.ClassBitField&flagBigEndian))
	return want != nil && bytes.Equal(dt.Properties, want)
}
