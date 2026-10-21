package hdf5_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scigolib/hdf5"
	"github.com/scigolib/hdf5/internal/core"
)

// This file holds the whole-file fuzz target. It lives in the root package
// because internal/core cannot import its parent: only here can the fuzzer
// drive the public entry point (Open -> Walk -> Read) that issue #68 crashed.
//
// The property under test is ADR-003: any byte sequence must either open and
// read, or return an error. It must never panic, recurse without bound, or ask
// for an allocation that Go turns into a fatal runtime throw. There is no
// recover() in this file on purpose — a panic must fail the fuzz run and leave
// its reproducer behind.

// fuzzSeedFiles are small, real fixtures covering the formats the bounded code
// touches: traditional symbol tables (v0), modern object headers, contiguous
// and chunked layouts, compact and dense attributes, vlen strings, compound
// data and dense links (fractal heap). Paths are relative to the package
// directory, which is the working directory of `go test`.
var fuzzSeedFiles = []string{
	"testdata/minimal.h5",
	"testdata/simple_float64.h5",
	"testdata/v0.h5",
	"testdata/with_attributes.h5",
	"testdata/compound_test.h5",
	"testdata/vlen_strings.h5",
	"testdata/dense_links.h5",
	"testdata/reference/none.h5",
}

// Limits keep a single fuzz input from spending its budget walking or reading a
// pathological file. They bound work, not memory: memory is bounded by the
// reader-size checks in internal/core and internal/utils.
const (
	maxFuzzObjects  = 512
	maxFuzzDatasets = 4
	maxFuzzAttrs    = 8
)

// FuzzOpenFile writes the input to a temporary file and runs the public read
// path over it. Every input must either fail with an error or be readable; the
// values that come back must not be larger than the input that produced them,
// which pins the allocation property of ADR-003 from the outside.
func FuzzOpenFile(f *testing.F) {
	for _, name := range fuzzSeedFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatalf("read seed %s: %v", name, err)
		}
		f.Add(data)
	}

	// Degenerate inputs: empty, a valid signature with nothing behind it, and
	// plain text that is not an HDF5 file at all.
	f.Add([]byte{})
	f.Add([]byte("\x89HDF\r\n\x1a\n"))
	f.Add([]byte("not an HDF5 file"))

	// One directory per worker process; the file name is reused because the
	// worker executes one input at a time.
	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(dir, "f.h5")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write fuzz input: %v", err)
		}

		// A corrupted byte must not turn into an unbounded loop either: an
		// infinite loop would only show up as a stalled worker and silence in
		// the progress line, so it is failed explicitly here. The budget is
		// generous for files of a few kilobytes. The work runs off the test
		// goroutine so the budget can fire; a violation travels back as a
		// string and is failed here, on the test goroutine.
		done := make(chan string, 1)
		go func() {
			file, err := hdf5.Open(path)
			if err != nil {
				// Corruption that is detected is the desired outcome.
				done <- ""
				return
			}
			defer func() { _ = file.Close() }()

			done <- exerciseFuzzFile(len(data), file)
		}()

		select {
		case violation := <-done:
			if violation != "" {
				t.Fatal(violation)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("input did not finish in 5s")
		}
	})
}

// exerciseFuzzFile walks the file and reads the metadata and data it exposes.
// Errors from corrupt files are expected and discarded; the returned string is
// non-empty only when a bounds invariant is violated.
func exerciseFuzzFile(inputLen int, file *hdf5.File) string {
	objects := 0
	datasets := 0
	violation := ""

	file.Walk(func(_ string, obj hdf5.Object) {
		objects++
		if objects > maxFuzzObjects || violation != "" {
			return
		}

		switch o := obj.(type) {
		case *hdf5.Dataset:
			datasets++
			if datasets > maxFuzzDatasets {
				return
			}
			violation = exerciseFuzzDataset(inputLen, o)
		case *hdf5.Group:
			attrs, err := o.Attributes()
			if err == nil {
				violation = exerciseFuzzAttributes(inputLen, attrs)
			}
		case *hdf5.NamedDatatype:
			_ = o.Datatype()
		}
	})

	return violation
}

// exerciseFuzzDataset runs every public reader over one dataset. The readers
// return an error when the datatype or layout does not match, and that is
// expected; what matters is that they return.
func exerciseFuzzDataset(inputLen int, ds *hdf5.Dataset) string {
	if values, err := ds.Read(); err == nil {
		if violation := checkFuzzBound(inputLen, "Dataset.Read", len(values)); violation != "" {
			return violation
		}
	}
	if values, err := ds.ReadStrings(); err == nil {
		if violation := checkFuzzBound(inputLen, "Dataset.ReadStrings", len(values)); violation != "" {
			return violation
		}
	}
	if values, err := ds.ReadCompound(); err == nil {
		if violation := checkFuzzBound(inputLen, "Dataset.ReadCompound", len(values)); violation != "" {
			return violation
		}
	}
	if values, err := ds.ReadVLenBytes(); err == nil {
		if violation := checkFuzzBound(inputLen, "Dataset.ReadVLenBytes", len(values)); violation != "" {
			return violation
		}
	}
	_, _ = ds.Info()

	attrs, err := ds.Attributes()
	if err != nil {
		return ""
	}

	return exerciseFuzzAttributes(inputLen, attrs)
}

// exerciseFuzzAttributes resolves the first attributes of an object.
func exerciseFuzzAttributes(inputLen int, attrs []*core.Attribute) string {
	if len(attrs) > maxFuzzAttrs {
		attrs = attrs[:maxFuzzAttrs]
	}

	for _, attr := range attrs {
		value, err := attr.ReadValue()
		if err != nil {
			continue
		}
		if violation := checkFuzzValueBound(inputLen, "Attribute.ReadValue", value); violation != "" {
			return violation
		}
	}

	return ""
}

// checkFuzzBound reports an input when a reader returns more elements than the
// file has bytes. Every element of these results is decoded from at least one
// input byte (element sizes below one byte are not representable in HDF5), so a
// longer result means an allocation escaped the metadata bounds.
func checkFuzzBound(inputLen int, what string, n int) string {
	if n > inputLen {
		return fmt.Sprintf("%s returned %d elements from a %d-byte input", what, n, inputLen)
	}

	return ""
}

// checkFuzzValueBound applies the same rule to attribute values, including the
// slice types ReadValue can produce.
func checkFuzzValueBound(inputLen int, what string, value interface{}) string {
	switch v := value.(type) {
	case []int32:
		return checkFuzzBound(inputLen, what, len(v))
	case []int64:
		return checkFuzzBound(inputLen, what, len(v))
	case []float32:
		return checkFuzzBound(inputLen, what, len(v))
	case []float64:
		return checkFuzzBound(inputLen, what, len(v))
	case []string:
		return checkFuzzBound(inputLen, what, len(v))
	case []interface{}:
		return checkFuzzBound(inputLen, what, len(v))
	}

	return ""
}
