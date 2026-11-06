package hdf5_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/scigolib/hdf5"
)

// ExampleFileWriter_CreateCompound writes a table of key/value records, two
// fixed-length strings per record, and reads it back.
func ExampleFileWriter_CreateCompound() {
	dir, err := os.MkdirTemp("", "hdf5-compound")
	if err != nil {
		log.Fatal(err)
	}
	file := filepath.Join(dir, "settings.h5")

	// struct { char key[16]; char value[16]; }, laid out without gaps.
	kv, err := hdf5.NewPackedCompoundType([]hdf5.CompoundMember{
		{Name: "key", Type: hdf5.FixedString{Size: 16}},
		{Name: "value", Type: hdf5.FixedString{Size: 16, Charset: hdf5.CharsetUTF8}},
	})
	if err != nil {
		log.Fatal(err)
	}

	fw, err := hdf5.CreateForWrite(file, hdf5.CreateTruncate)
	if err != nil {
		log.Fatal(err)
	}
	ds, err := fw.CreateCompound("/settings", kv, []uint64{2})
	if err != nil {
		log.Fatal(err)
	}
	err = ds.Write([]map[string]any{
		{"key": "mode", "value": "fast"},
		{"key": "owner", "value": "Zoë"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		log.Fatal(err)
	}

	f, err := hdf5.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	f.Walk(func(path string, obj hdf5.Object) {
		d, ok := obj.(*hdf5.Dataset)
		if !ok {
			return
		}
		rows, err := d.ReadCompound()
		if err != nil {
			log.Fatal(err)
		}
		for _, r := range rows {
			fmt.Printf("%s: %s = %s\n", path, r["key"], r["value"])
		}
	})
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Fatal(err)
	}
	// Output:
	// /settings: mode = fast
	// /settings: owner = Zoë
}

// ExampleNewCompoundType lays out members at explicit offsets, with padding,
// as a C struct with alignment would.
func ExampleNewCompoundType() {
	point, err := hdf5.NewCompoundType(16, []hdf5.CompoundMember{
		{Name: "id", Offset: 0, Type: hdf5.Int32},
		{Name: "value", Offset: 8, Type: hdf5.Float64}, // 4 bytes of padding before
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range point.Members() {
		fmt.Println(m.Name, m.Offset)
	}
	fmt.Println("size", point.Size())

	_, err = hdf5.NewCompoundType(8, []hdf5.CompoundMember{
		{Name: "a", Offset: 0, Type: hdf5.Int32},
		{Name: "b", Offset: 2, Type: hdf5.Int32},
	})
	fmt.Println(err)
	// Output:
	// id 0
	// value 8
	// size 16
	// compound member "b" at [2,6) overlaps member "a" at [0,4)
}
