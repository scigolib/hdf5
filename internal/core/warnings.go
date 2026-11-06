package core

import (
	"fmt"
	"sync"
)

// WarningCode classifies a finding that does not stop a file from being read
// but that its user should know about: structures only an earlier hdf5-go
// release writes, which the HDF5 C library cannot open, or a checksum that
// does not match.
type WarningCode uint8

// Warning codes. The values are part of the public API of the root package
// (hdf5.WarningCode), so they are never renumbered.
const (
	// WarnLegacySuperblockChecksum: the version 2/3 superblock carries
	// CRC32-IEEE instead of lookup3 (hdf5-go up to v0.13.4, commit 01a277d).
	WarnLegacySuperblockChecksum WarningCode = iota + 1
	// WarnLegacyMetadataChecksum: a B-tree v2 or fractal heap block carries
	// CRC32-IEEE instead of lookup3 (hdf5-go up to v0.13.4, commit 01a277d).
	WarnLegacyMetadataChecksum
	// WarnHeaderChecksum: a version 2 object header chunk (OHDR or OCHK) has
	// no valid checksum (hdf5-go up to v0.13.8 wrote none, or the file is
	// corrupted).
	WarnHeaderChecksum
	// WarnHeaderChunkSize: a version 2 object header whose chunk size counts
	// its checksum (hdf5-go v0.13.9, fixed in commit 2f92dd0).
	WarnHeaderChunkSize
	// WarnLegacyDenseLayout: dense attribute storage in the layout hdf5-go up
	// to v0.14.2 wrote.
	WarnLegacyDenseLayout
	// WarnLegacyDatatype: a numeric datatype in the encoding hdf5-go
	// v0.11.0-beta to v0.13.9 wrote (fixed in commit 2f92dd0).
	WarnLegacyDatatype
	// WarnSuppressed: the warning limit was reached; later findings are
	// counted, not listed.
	WarnSuppressed
)

// Warning is one finding recorded while a file was read.
type Warning struct {
	Code    WarningCode
	Addr    uint64 // File address of the structure, or 0 for the file as a whole.
	Message string
}

// legacyDenseLayoutMessage is the warning text for dense storage in the
// layout of hdf5-go up to v0.14.2; its arguments name the structure, its
// address and what marks it as legacy.
const legacyDenseLayoutMessage = "%s at 0x%X is in the dense storage layout hdf5-go v0.14.2 and earlier wrote (%s); " +
	"the HDF5 C library cannot read it. Changing the object's attributes through OpenForWrite migrates dense " +
	"attribute storage to the C layout; otherwise rewrite the file"

// maxWarnings bounds the warnings kept per file: the number of findings is
// controlled by the file, the memory spent on them is not (ADR-003). One more
// entry, WarnSuppressed, reports how many were dropped.
const maxWarnings = 1000

type warningKey struct {
	code WarningCode
	addr uint64
}

// warningSink collects the warnings of one file, one per code and address.
type warningSink struct {
	mu         sync.Mutex
	seen       map[warningKey]struct{}
	list       []Warning
	suppressed int
	spans      []ChecksumSpan // legacy checksums recorded in list, for RepairLegacyChecksums
	// spansIncomplete is set when a legacy checksum finding was suppressed
	// past maxWarnings: its span was not kept, so spans cannot repair the file.
	spansIncomplete bool
	// codes marks every code ever reported, suppressed ones included: the
	// write path refuses a file by code, and a finding dropped past
	// maxWarnings must still refuse it. One bit per possible code, so the
	// memory does not depend on the file.
	codes [256]bool
}

// ChecksumSpan is a metadata block whose checksum is CRC32-IEEE: lookup3 over
// [Addr, Addr+Len) belongs in the four bytes at Addr+Len.
type ChecksumSpan struct {
	Addr, Len uint64
}

// Warnf records a warning for the structure at addr. A repeated code and
// address is recorded once; past maxWarnings, warnings are only counted.
// A nil superblock records nothing.
func (sb *Superblock) Warnf(code WarningCode, addr uint64, format string, args ...any) {
	sb.recordf(code, addr, 0, format, args...)
}

// warnLegacyChecksumf is Warnf for a block whose checksum over [addr, addr+n)
// is CRC32-IEEE, remembering the span so the write path can repair it.
func (sb *Superblock) warnLegacyChecksumf(code WarningCode, addr, n uint64, format string, args ...any) {
	sb.recordf(code, addr, n, format, args...)
}

func (sb *Superblock) recordf(code WarningCode, addr, span uint64, format string, args ...any) {
	if sb == nil {
		return
	}
	s := &sb.warnings
	s.mu.Lock()
	defer s.mu.Unlock()

	s.codes[code] = true
	key := warningKey{code, addr}
	if _, dup := s.seen[key]; dup {
		return
	}
	if len(s.list) >= maxWarnings {
		s.suppressed++
		if span > 0 {
			s.spansIncomplete = true
		}
		return
	}
	if s.seen == nil {
		s.seen = make(map[warningKey]struct{})
	}
	s.seen[key] = struct{}{}
	s.list = append(s.list, Warning{Code: code, Addr: addr, Message: fmt.Sprintf(format, args...)})
	if span > 0 {
		s.spans = append(s.spans, ChecksumSpan{Addr: addr, Len: span})
	}
}

// LegacyChecksums returns the blocks found so far whose checksum is
// CRC32-IEEE, and whether the list is complete: it is not when a legacy
// checksum finding was suppressed past maxWarnings, since a suppressed finding
// keeps no span.
func (sb *Superblock) LegacyChecksums() (spans []ChecksumSpan, complete bool) {
	if sb == nil {
		return nil, true
	}
	s := &sb.warnings
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]ChecksumSpan(nil), s.spans...), !s.spansIncomplete
}

// HasWarning reports whether code was reported for this file so far,
// including findings suppressed past maxWarnings.
func (sb *Superblock) HasWarning(code WarningCode) bool {
	if sb == nil {
		return false
	}
	s := &sb.warnings
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.codes[code]
}

// Warnings returns a copy of the warnings recorded so far, with a final
// WarnSuppressed entry when some were dropped.
func (sb *Superblock) Warnings() []Warning {
	if sb == nil {
		return nil
	}
	s := &sb.warnings
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.list) == 0 {
		return nil
	}
	out := make([]Warning, len(s.list), len(s.list)+1)
	copy(out, s.list)
	if s.suppressed > 0 {
		out = append(out, Warning{
			Code:    WarnSuppressed,
			Message: fmt.Sprintf("%d more warnings were suppressed after the first %d", s.suppressed, maxWarnings),
		})
	}
	return out
}
