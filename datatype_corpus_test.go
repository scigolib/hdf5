package hdf5

// Byte-exact check of the datatype codec against datatype messages the HDF5 C
// library wrote: every datatype message of every dataset and committed
// datatype in the C-written test files is decoded, encoded again, and must
// come out byte for byte as the C library wrote it (H5O__dtype_encode_helper,
// H5Odtype.c:932-1446). The same walk is the decoder's regression guard: a
// message the old reader accepted and the new decoder rejects is listed.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// datatypeCorpusResult counts what the corpus walk found.
type datatypeCorpusResult struct {
	messages  int            // datatype messages seen
	identical int            // re-encoded byte for byte
	upgraded  int            // version-1 compounds with array members, re-encoded as version 2
	perClass  map[string]int // identical messages per top-level class
	rejected  []string       // messages the decoder rejected
	invalid   int            // messages of knownInvalidDatatypes, rejected as corrupt
	skipped   []string       // corpus files the reader cannot open
	skippedOH int            // object headers that could not be read
	mismatch  []string       // decoded but re-encoded differently
	nested    map[string]int // classes seen anywhere in a decoded type
}

// corpusFiles lists the C-written test files of the official suite and the
// reference set.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range []string{"testdata/hdf5_official/*.h5", "testdata/reference/*.h5"} {
		m, err := filepath.Glob(pattern)
		require.NoError(t, err)
		files = append(files, m...)
	}
	sort.Strings(files)
	require.NotEmpty(t, files)
	return files
}

// knownInvalidDatatypes are corpus files whose datatype messages the C
// library rejects on purpose: bad_compound.h5 holds compounds without
// members (gen_bad_compound.c; dtypes.c:3847-3870 expects H5Topen2 and
// H5Dopen2 to fail, H5Odtype.c:358-360).
var knownInvalidDatatypes = map[string]bool{"bad_compound.h5": true}

// corpusObject is an object of a corpus file with its own object header.
type corpusObject struct {
	path string
	kind string // "dataset", "datatype" or "group"
	addr uint64
}

// corpusObjects returns the datasets, committed datatypes and groups with an
// object header address reachable in f, each address once.
func corpusObjects(f *File) []corpusObject {
	seen := map[uint64]bool{}
	var objs []corpusObject
	f.Walk(func(path string, obj Object) {
		o := corpusObject{path: path}
		switch v := obj.(type) {
		case *Dataset:
			o.kind, o.addr = "dataset", v.Address()
		case *NamedDatatype:
			o.kind, o.addr = "datatype", v.address
		case *Group:
			o.kind, o.addr = "group", v.address
		default:
			return
		}
		if o.addr != 0 && !seen[o.addr] {
			seen[o.addr] = true
			objs = append(objs, o)
		}
	})
	return objs
}

func classLabel(dt *core.DatatypeMessage) string {
	return fmt.Sprintf("class_%d", dt.Class)
}

// forEachHeaderMessage calls fn with every object header message of the
// datasets, committed datatypes and groups of the corpus files. It returns
// the files it could not open and the object headers it could not read, so
// callers can assert how much of the corpus was skipped.
func forEachHeaderMessage(t *testing.T, fn func(file string, obj corpusObject, msg *core.HeaderMessage)) (skippedFiles []string, skippedHeaders int) {
	t.Helper()
	for _, file := range corpusFiles(t) {
		f, err := Open(file)
		if err != nil {
			skippedFiles = append(skippedFiles, filepath.Base(file)) // covered by the suite tests
			continue
		}
		for _, obj := range corpusObjects(f) {
			oh, err := core.ReadObjectHeader(f.Reader(), obj.addr, f.Superblock())
			if err != nil {
				skippedHeaders++
				continue
			}
			for _, msg := range oh.Messages {
				fn(filepath.Base(file), obj, msg)
			}
		}
		require.NoError(t, f.Close())
	}
	return skippedFiles, skippedHeaders
}

// forEachDatatypeMessage calls fn with every datatype message of a dataset
// or committed datatype in the corpus files that is not a shared-message
// reference (message flag H5O_MSG_FLAG_SHARED, H5Oprivate.h:74).
func forEachDatatypeMessage(t *testing.T, fn func(where string, data []byte)) (skippedFiles []string, skippedHeaders int) {
	t.Helper()
	return forEachHeaderMessage(t, func(file string, obj corpusObject, msg *core.HeaderMessage) {
		if obj.kind != "group" && msg.Type == core.MsgDatatype && msg.Flags&core.MsgFlagShared == 0 {
			fn(fmt.Sprintf("%s @0x%x", file, obj.addr), msg.Data)
		}
	})
}

func walkDatatypeCorpus(t *testing.T) datatypeCorpusResult {
	t.Helper()
	res := datatypeCorpusResult{perClass: map[string]int{}, nested: map[string]int{}}
	res.skipped, res.skippedOH = forEachDatatypeMessage(t, func(where string, data []byte) {
		res.messages++
		res.check(where, data)
	})
	return res
}

// TestSharedDatatypeCensus lists the shared (committed) datatypes of the
// corpus: dataset datatype messages with the shared message flag, and
// attribute messages whose datatype is shared (attribute flag bit 0,
// H5O_ATTR_FLAG_TYPE_SHARED, H5Oattr.c). Attributes in dense storage are not
// visited. Shared messages are valid HDF5: decoding one reports "not
// supported yet", never corruption.
func TestSharedDatatypeCensus(t *testing.T) {
	var datasets, attrs []string
	_, _ = forEachHeaderMessage(t, func(file string, obj corpusObject, msg *core.HeaderMessage) {
		switch {
		case msg.Type == core.MsgDatatype && msg.Flags&core.MsgFlagShared != 0:
			datasets = append(datasets, fmt.Sprintf("%s %s %s", file, obj.kind, obj.path))
			_, err := core.ParseDatatypeHeaderMessage(msg)
			require.ErrorContains(t, err, "committed (shared) datatype not supported yet")
			require.NotErrorIs(t, err, ErrCorrupt)
		case msg.Type == core.MsgAttribute && len(msg.Data) > 1 && msg.Data[1]&0x01 != 0:
			attrs = append(attrs, fmt.Sprintf("%s %s %s", file, obj.kind, obj.path))
			_, err := core.ParseAttributeMessage(msg.Data, binary.LittleEndian)
			require.ErrorContains(t, err, "committed (shared) datatype not supported yet")
			require.NotErrorIs(t, err, ErrCorrupt)
		}
	})
	t.Logf("shared datatype messages of objects: %d", len(datasets))
	for _, d := range datasets {
		t.Logf("  object: %s", d)
	}
	t.Logf("attribute messages with a shared datatype: %d", len(attrs))
	for _, a := range attrs {
		t.Logf("  attribute on: %s", a)
	}
}

// check decodes one C-written message and re-encodes it.
func (res *datatypeCorpusResult) check(where string, data []byte) {
	dt, n, err := core.DecodeDatatype(data)
	file := strings.Fields(where)[0]
	if knownInvalidDatatypes[file] {
		// The C library rejects these too; they must stay corruption.
		if !errors.Is(err, ErrCorrupt) {
			res.mismatch = append(res.mismatch, fmt.Sprintf("%s: expected ErrCorrupt, got %v", where, err))
		} else {
			res.invalid++
		}
		return
	}
	if err != nil {
		res.rejected = append(res.rejected, fmt.Sprintf("%s: %v", where, err))
		return
	}
	enc, err := core.EncodeDatatypeMessage(dt)
	if err != nil {
		res.mismatch = append(res.mismatch, fmt.Sprintf("%s: re-encode failed: %v", where, err))
		return
	}
	if stored := data[0] >> 4; dt.Version != stored {
		// The decoder raised the version as C does (version-1 compounds
		// with array members become version 2, H5Odtype.c:526-574,
		// :622-630), so the bytes C would write now are the upgraded
		// encoding; it must be stable and keep the members.
		if err := checkUpgradedCompound(dt, enc); err != nil {
			res.mismatch = append(res.mismatch, fmt.Sprintf("%s: upgraded v%d compound: %v", where, stored, err))
			return
		}
		res.upgraded++
		return
	}
	if !bytes.Equal(enc, data[:n]) {
		res.mismatch = append(res.mismatch, fmt.Sprintf("%s: %s\n  file:    % x\n  encoded: % x", where, dt, data[:n], enc))
		return
	}
	res.identical++
	res.perClass[classLabel(dt)]++
	countNested(dt, res.nested)
}

// checkUpgradedCompound requires the upgraded encoding enc of dt to be a
// version-2 compound that decodes to the same members and encodes again to
// the same bytes.
func checkUpgradedCompound(dt *core.DatatypeMessage, enc []byte) error {
	back, _, err := core.DecodeDatatype(enc)
	if err != nil {
		return err
	}
	if back.Version != 2 || enc[0]>>4 != 2 {
		return fmt.Errorf("upgraded to version %d, want 2", back.Version)
	}
	again, err := core.EncodeDatatypeMessage(back)
	if err != nil {
		return fmt.Errorf("re-encode upgraded type: %w", err)
	}
	if !bytes.Equal(again, enc) {
		return errors.New("upgraded encoding not stable")
	}
	if back.Size != dt.Size || len(back.Members) != len(dt.Members) {
		return fmt.Errorf("size/members %d/%d, want %d/%d", back.Size, len(back.Members), dt.Size, len(dt.Members))
	}
	for i, m := range back.Members {
		w := dt.Members[i]
		if m.Name != w.Name || m.Offset != w.Offset || m.Type.Class != w.Type.Class || m.Type.Size != w.Type.Size {
			return fmt.Errorf("member %d: %s@%d %s, want %s@%d %s", i, m.Name, m.Offset, m.Type, w.Name, w.Offset, w.Type)
		}
	}
	return nil
}

func countNested(dt *core.DatatypeMessage, into map[string]int) {
	into[classLabel(dt)]++
	for _, m := range dt.Members {
		countNested(m.Type, into)
	}
	if dt.Base != nil {
		countNested(dt.Base, into)
	}
}

// TestWriteDatatypeFuzzSeeds regenerates the seed corpora of the datatype fuzz
// targets (internal/core/datatype_fuzz_test.go) from C-written datatype
// messages: up to 6 distinct messages per class and version, shortest first,
// plus two shared-message references (bodies of messages with the shared
// flag, which the decoder must reject without panicking). It only runs with
// HDF5_WRITE_FUZZ_SEEDS=1:
//
//	HDF5_WRITE_FUZZ_SEEDS=1 go test -run TestWriteDatatypeFuzzSeeds .
func TestWriteDatatypeFuzzSeeds(t *testing.T) {
	if os.Getenv("HDF5_WRITE_FUZZ_SEEDS") != "1" {
		t.Skip("set HDF5_WRITE_FUZZ_SEEDS=1 to regenerate the datatype fuzz seeds")
	}
	byKey := map[string][][]byte{}
	seen := map[string]bool{}
	_, _ = forEachHeaderMessage(t, func(_ string, obj corpusObject, msg *core.HeaderMessage) {
		data := msg.Data
		if obj.kind == "group" || msg.Type != core.MsgDatatype || seen[string(data)] {
			return
		}
		seen[string(data)] = true
		key := "shared"
		if msg.Flags&core.MsgFlagShared == 0 {
			if dt, n, err := core.DecodeDatatype(data); err != nil {
				key = "invalid" // knownInvalidDatatypes
			} else {
				key = fmt.Sprintf("class%d-v%d", dt.Class, dt.Version)
				data = data[:n]
			}
		}
		byKey[key] = append(byKey[key], append([]byte(nil), data...))
	})
	for _, target := range []string{"FuzzDatatypeDecode", "FuzzDatatypeRoundTrip"} {
		dir := filepath.Join("internal", "core", "testdata", "fuzz", target)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		written := 0
		for key, msgs := range byKey {
			written += writeFuzzSeeds(t, dir, key, msgs)
		}
		t.Logf("%s: %d seeds", dir, written)
	}
}

// writeFuzzSeeds writes the shortest messages of one class/version group as
// fuzz corpus files and returns how many it wrote.
func writeFuzzSeeds(t *testing.T, dir, key string, msgs [][]byte) int {
	t.Helper()
	sort.Slice(msgs, func(i, j int) bool { return len(msgs[i]) < len(msgs[j]) })
	limit := 6
	if key == "shared" || key == "invalid" {
		limit = 2
	}
	msgs = msgs[:min(limit, len(msgs))]
	for i, m := range msgs {
		body := fmt.Sprintf("go test fuzz v1\n[]byte(%q)\n", m)
		name := filepath.Join(dir, fmt.Sprintf("c-%s-%d", key, i))
		require.NoError(t, os.WriteFile(name, []byte(body), 0o600))
	}
	return len(msgs)
}

// TestDatatypeCodec_CWrittenByteExact requires every C-written datatype
// message that decodes to re-encode to the identical bytes.
func TestDatatypeCodec_CWrittenByteExact(t *testing.T) {
	res := walkDatatypeCorpus(t)

	t.Logf("datatype messages: %d, byte-identical: %d, v1 compounds with array members upgraded: %d, known invalid rejected: %d",
		res.messages, res.identical, res.upgraded, res.invalid)
	t.Logf("top-level classes: %v", res.perClass)
	t.Logf("classes anywhere in a type: %v", res.nested)
	for _, r := range res.rejected {
		t.Errorf("C-written datatype rejected: %s", r)
	}
	for _, m := range res.mismatch {
		t.Errorf("not byte-identical: %s", m)
	}
	// Coverage floors: the test must not pass on a corpus that silently
	// shrank. Numbers measured 2026-11-06 on testdata/hdf5_official +
	// testdata/reference; the corpus has no time datatype (class 2).
	t.Logf("skipped files: %v, unreadable object headers: %d", res.skipped, res.skippedOH)
	require.Len(t, res.skipped, corpusSkippedFiles, "corpus files the reader cannot open")
	require.Equal(t, corpusSkippedHeaders, res.skippedOH, "unreadable object headers")
	require.GreaterOrEqual(t, res.messages, 1554)
	require.GreaterOrEqual(t, res.identical, 1542)
	require.Equal(t, 8, res.upgraded)
	for class, floor := range map[core.DatatypeClass]int{
		core.DatatypeFixed: 870, core.DatatypeFloat: 292, core.DatatypeString: 37,
		core.DatatypeBitfield: 20, core.DatatypeOpaque: 15, core.DatatypeCompound: 139,
		core.DatatypeReference: 39, core.DatatypeEnum: 16, core.DatatypeVarLen: 48,
		core.DatatypeArray: 48, core.DatatypeComplex: 18,
	} {
		require.GreaterOrEqual(t, res.perClass[fmt.Sprintf("class_%d", class)], floor,
			"byte-identical C-written datatypes of class %d", class)
	}
}

// Skipped parts of the corpus (files the reader cannot open, object headers
// it cannot read), measured 2026-11-06; a change means the corpus coverage
// changed and the floors above need a look.
const (
	// Mostly family/multi-file members and known-invalid files. 65 before the
	// version-2 header gap fix of TASK-058 opened torderattr.h5 and h5stat_tsohm.h5;
	// 63 before its fractal heap reader found the dense links of h5stat_newgrat.h5
	// and h5repack_objs.h5 through indirect blocks.
	corpusSkippedFiles   = 61
	corpusSkippedHeaders = 0
)
