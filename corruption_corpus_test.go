package hdf5_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scigolib/hdf5"
)

// This file is the deterministic half of the issue #68 safety net. The fuzz
// targets explore; this test replays one fixed mutation per metadata byte, so a
// regression shows up in a normal `go test ./...` run, without a fuzz session.
//
// It lives in the root package because the property is stated at the public
// boundary (Open + Walk + Read); internal/core cannot import its parent.

// corpusBudget bounds the whole test. The default suite must stay fast, so the
// loop stops at the deadline and logs how far it got instead of mutating every
// byte of a larger fixture.
const corpusBudget = 5 * time.Second

// corpusFixture is one file mutated by TestCorruptionCorpus: limit caps the
// metadata region (0 means the whole file), and share is the portion of the
// total budget the fixture may use.
type corpusFixture struct {
	name  string
	limit int
	share int
}

// corpusFixtures covers the smallest modern file with one dataset and a file
// with compact attributes; the attribute region sits inside the first 4 KB.
var corpusFixtures = []corpusFixture{
	{name: "testdata/minimal.h5", limit: 0, share: 50},
	{name: "testdata/with_attributes.h5", limit: 4096, share: 50},
}

// TestCorruptionCorpus flips one metadata byte at a time (b[i] ^= 0xFF) and
// requires every mutant to either open or return an error. A returned error is
// a pass: corruption that is detected is the desired behavior (ADR-003). A
// panic, an out-of-bounds access or a fatal allocation is a failure, and no
// recover() is used here so that it cannot be hidden.
func TestCorruptionCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corruption corpus is skipped in -short mode")
	}

	start := time.Now()
	for _, fixture := range corpusFixtures {
		t.Run(filepath.Base(fixture.name), func(t *testing.T) {
			remaining := corpusBudget - time.Since(start)
			budget := remaining * time.Duration(fixture.share) / 100
			mutateCorpus(t, fixture.name, fixture.limit, budget)
		})
	}
}

// mutateCorpus applies b[i] ^= 0xFF for every byte in the selected region until
// the budget runs out, opening and walking each mutant in turn.
func mutateCorpus(t *testing.T, name string, limit int, budget time.Duration) {
	t.Helper()

	original, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	// The unmutated file must be readable, otherwise every mutant below would
	// be rejected for the wrong reason and the test would prove nothing.
	verifyFixtureOpens(t, name, original)

	if limit <= 0 || limit > len(original) {
		limit = len(original)
	}

	path := filepath.Join(t.TempDir(), "mutant.h5")
	start := time.Now()
	deadline := start.Add(budget)
	mutants := 0

	// Visit the region with decreasing strides: if the budget runs out early,
	// the bytes tested are spread across the whole file instead of covering
	// only its prefix (the superblock), and the object headers still get hit.
	seen := make([]bool, limit)

walk:
	for _, stride := range []int{16, 8, 4, 2, 1} {
		for i := 0; i < limit; i += stride {
			if seen[i] {
				continue
			}
			if mutants > 0 && time.Now().After(deadline) {
				break walk
			}
			seen[i] = true

			violation, err := mutateOneByte(original, i, path)
			if err != nil {
				t.Fatalf("mutant %d: %v", i, err)
			}
			if violation != "" {
				t.Fatalf("mutant %d: %s", i, violation)
			}
			mutants++
		}
	}

	t.Logf("mutated %d of %d metadata bytes in %s", mutants, limit, time.Since(start))
}

// mutateOneByte writes the mutant with one flipped bit pattern, opens it and
// walks it. An error from Open is a pass (corruption detected); the returned
// violation is non-empty only when a reader broke a bounds invariant, and the
// error is non-nil only when the mutant cannot be written.
func mutateOneByte(original []byte, offset int, path string) (string, error) {
	mutant := make([]byte, len(original))
	copy(mutant, original)
	mutant[offset] ^= 0xFF

	if err := os.WriteFile(path, mutant, 0o600); err != nil {
		return "", err
	}

	file, ok := openMutant(path)
	if !ok {
		// Corruption that is detected is a pass.
		return "", nil
	}

	violation := exerciseFuzzFile(len(mutant), file)
	_ = file.Close()

	return violation, nil
}

// openMutant reports whether the mutant is a readable HDF5 file.
func openMutant(path string) (*hdf5.File, bool) {
	file, err := hdf5.Open(path)
	if err != nil {
		return nil, false
	}

	return file, true
}

// verifyFixtureOpens checks that the chosen fixture is a valid file.
func verifyFixtureOpens(t *testing.T, name string, data []byte) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "original.h5")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}

	file, err := hdf5.Open(path)
	if err != nil {
		t.Fatalf("fixture %s does not open: %v", name, err)
	}
	_ = file.Close()
}
