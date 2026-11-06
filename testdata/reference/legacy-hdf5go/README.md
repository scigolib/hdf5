# Files written by hdf5-go before v0.14.3

These files are NOT valid HDF5: the HDF5 C library (h5dump 1.14.6) refuses their datatypes. They
were written by this library at commit 257fb7d (main after v0.14.2), before the datatype codec of
v0.14.3, and are kept so the reader keeps reading what users already wrote (legacy read paths in
`internal/core/datatype_codec.go`, ADR-004).

| File | Written with | C error |
|---|---|---|
| `numeric.h5` | `core.CreateBasicDatatypeMessage` + `core.CreateCompoundTypeFromFields` + `FileWriter.CreateCompoundDataset` + `WriteRaw`: 2 records `{id uint32 @0, value float64 @4}` = (1, 0.5), (2, 1.5) | invalid number of members: 0 |
| `with_string.h5` | same API: 2 records `{id uint32, value float64, tag string[8] @12}` = (1, 0.5, "row0"), (2, 1.5, "row1") | invalid number of members: 0 |
| `kv_strings.h5` | same API: 1 record `{key string[16] @0, value string[16] @16}` = ("detector", "H1") | invalid number of members: 0 |
| `float_attrs.h5` | `CreateForWrite`, `CreateDataset("/d", Int32, [1])` = 7, `WriteAttribute("gain", float64(1.5))`, `WriteAttribute("weights", []float32{0.25, 0.75})` | mantissa and sign positions overlap ("error getting attribute information") |

## MATLAB v7.3 files of hdf5-go v0.11.4-beta

Copied from `github.com/scigolib/matlab` `testdata/generated/` (commit c31eed6, 2025-11-02, which
required hdf5 v0.11.4-beta). They carry every early-format finding at once:

- a CRC32-IEEE superblock checksum (hdf5-go up to v0.13.4; h5dump 1.14.6: `unable to open file`);
- version 2 object headers without checksums (up to v0.13.8);
- numeric datatypes from the encoder of v0.11.0-beta to v0.13.9 (`encodeDatatypeNumeric` before
  2f92dd0). That encoder stored byte order, precision and offset as single bytes, then exponent size,
  mantissa size and a one-byte bias of 127. The reader reads them as the IEEE or integer type meant
  (`isEarlyHDF5GoNumeric` in `internal/core/legacy_datatype.go`).

| File | Content |
|---|---|
| `matlab_simple_double.mat` | `/data` float64 [1 2 3 4 5], attribute `MATLAB_class` = "double" |
| `matlab_simple_int32.mat` | `/values` int32 [10 20 30 40], `MATLAB_class` = "int32" |
| `matlab_simple_single.mat` | `/floats` float32 [1.5 2.5 3.5], `MATLAB_class` = "single" |

## Dense attribute name index with link records

`dense_attr_type5.h5` was written by this library on branch `fix/v0.14.3-descriptor-codec` (HEAD
22cfabf, before the TASK-058 writer fix): `CreateForWrite`, `CreateDataset("/d", Float64, [2])` =
{1, 2}, then `WriteAttribute(fmt.Sprintf("a%02d", i), int32(i))` for i = 0..11. The attributes go to
dense storage, whose name index the writer gave the link name record type 5 (hash(4) + 7-byte heap
ID, 11-byte records) instead of the attribute name type 8 (H5Abtree2.c). The C library opens the
index with the type 8 class and refuses it: `h5dump -a /d/a03` prints `unable to open attribute
"a03"`. The reader accepts type 5 in that slot as this legacy layout (`denseIndexTypes` in
`internal/core/dense_heap.go`).

`dense_attr_v0142_sb0.h5` and `dense_attr_v0142_sb2.h5` were written by the released module
`github.com/scigolib/hdf5 v0.14.2` (a standalone program that requires that version):
`CreateForWrite(name, CreateTruncate, WithSuperblockVersion(SuperblockV0 or SuperblockV2))`,
`CreateDataset("/d", Int32, [4])` = {1, 2, 3, 4}, then `WriteAttribute(fmt.Sprintf("a%02d", i),
int32(i*7+1))` for i = 0..9. They carry dense attribute storage as v0.14.2 wrote it, the type 5
name index with 11-byte records and the attribute info message under ID 0x000F among it, together
with that release's fractal heap and object headers. `h5dump -a /d/a03` refuses both
(`unable to open attribute "a03"`). `TestDenseAttributes_LegacyIndexMigrated` migrates copies.
