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
