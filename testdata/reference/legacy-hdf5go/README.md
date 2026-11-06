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

## Dense attribute name index with link records

`dense_attr_type5.h5` was written by this library on branch `fix/v0.14.3-descriptor-codec` (HEAD
22cfabf, before the TASK-058 writer fix): `CreateForWrite`, `CreateDataset("/d", Float64, [2])` =
{1, 2}, then `WriteAttribute(fmt.Sprintf("a%02d", i), int32(i))` for i = 0..11. The attributes go to
dense storage, whose name index the writer gave the link name record type 5 (hash(4) + 7-byte heap
ID, 11-byte records) instead of the attribute name type 8 (H5Abtree2.c). The C library opens the
index with the type 8 class and refuses it: `h5dump -a /d/a03` prints `unable to open attribute
"a03"`. The reader accepts type 5 in that slot as this legacy layout (`denseIndexTypes` in
`internal/core/dense_heap.go`).
