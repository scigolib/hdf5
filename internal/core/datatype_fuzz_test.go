package core

import (
	"bytes"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
)

// Fuzz targets of the datatype codec (ADR-004 §5). The seed corpora under
// testdata/fuzz/FuzzDatatypeDecode and testdata/fuzz/FuzzDatatypeRoundTrip are
// datatype messages written by the HDF5 C library, taken from the official
// test suite by TestWriteDatatypeFuzzSeeds (root package). There is no
// recover(): a panic is a finding.

// addCodecSeeds adds the codec's own samples to a fuzz target.
func addCodecSeeds(f *testing.F) {
	f.Add([]byte{})
	f.Add(int32LEBytes)
	f.Add(uint8LEBytes)
	f.Add(float64LEBytes)
	f.Add(legacyF64Bytes)        // legacy hdf5-go float attribute encoding
	f.Add(legacyCompoundBytes()) // legacy hdf5-go compound encoding
	for _, dt := range []func() (*DatatypeMessage, error){
		func() (*DatatypeMessage, error) { return NewVLenStringDatatype(StringNullTerm, CharsetASCII) },
		func() (*DatatypeMessage, error) { return NewOpaqueDatatype(4, "tag") },
		func() (*DatatypeMessage, error) {
			i32, err := NewIntegerDatatype(4, true)
			if err != nil {
				return nil, err
			}
			return NewArrayDatatype(i32, []uint32{2, 3})
		},
		func() (*DatatypeMessage, error) {
			s, err := NewStringDatatype(8, StringNullPad, CharsetASCII)
			if err != nil {
				return nil, err
			}
			i32, err := NewIntegerDatatype(4, true)
			if err != nil {
				return nil, err
			}
			return NewCompoundDatatype(12, []CompoundMember{{Name: "s", Type: s}, {Name: "i", Offset: 8, Type: i32}})
		},
	} {
		msg, err := dt()
		if err != nil {
			f.Fatalf("seed: %v", err)
		}
		enc, err := EncodeDatatypeMessage(msg)
		if err != nil {
			f.Fatalf("seed: %v", err)
		}
		f.Add(enc)
	}
}

// FuzzDatatypeDecode checks that the decoder never panics, reports every
// failure as corruption, and never claims bytes it was not given.
func FuzzDatatypeDecode(f *testing.F) {
	addCodecSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		dt, n, err := DecodeDatatype(data)
		if err != nil {
			if !errors.Is(err, utils.ErrCorrupt) {
				t.Fatalf("decode error does not wrap ErrCorrupt: %v", err)
			}
			return
		}
		if n < datatypeHeaderSize || n > len(data) {
			t.Fatalf("decoded %d bytes of %d", n, len(data))
		}
		if !bytes.Equal(dt.Properties, data[datatypeHeaderSize:n]) {
			t.Fatalf("Properties are not the consumed bytes")
		}
		_ = dt.String()
		if dt.IsCompound() {
			if _, err := ParseCompoundType(dt); err != nil {
				t.Fatalf("decoded compound: %v", err)
			}
		}
	})
}

// FuzzDatatypeRoundTrip checks encode(decode(x)): whatever the encoder
// accepts decodes again, to a message that encodes to the same bytes. The
// encoder may refuse a decodable type (for example a version-1 compound with
// array members, which C upgrades before writing), never write one the decoder
// refuses.
func FuzzDatatypeRoundTrip(f *testing.F) {
	addCodecSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		dt, _, err := DecodeDatatype(data)
		if err != nil {
			return
		}
		enc, err := EncodeDatatypeMessage(dt)
		if err != nil {
			return
		}
		back, n, err := DecodeDatatype(enc)
		if err != nil {
			t.Fatalf("encoded message does not decode: %v", err)
		}
		if n != len(enc) {
			t.Fatalf("encoded %d bytes, decoded %d", len(enc), n)
		}
		again, err := EncodeDatatypeMessage(back)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		if !bytes.Equal(enc, again) {
			t.Fatalf("encoding is not stable:\n% x\n% x", enc, again)
		}
	})
}
