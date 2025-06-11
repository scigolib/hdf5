package hdf5

import (
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
)

type Object interface {
	Name() string
}

type Dataset struct {
	file *File
	name string
}

func (d *Dataset) Name() string {
	return d.name
}

type Group struct {
	file        *File
	name        string
	children    []Object
	symbolTable *structures.SymbolTable
}

func (g *Group) Name() string {
	return g.name
}

func (g *Group) Children() []Object {
	return g.children
}

func loadGroup(file *File, address uint64) (*Group, error) {
	if address == 0 {
		return nil, errors.New("invalid group address: 0")
	}

	r := file.osFile
	sb := file.sb

	header, err := core.ReadObjectHeader(r, address, sb)
	if err != nil {
		return nil, utils.WrapError("object header read failed", err)
	}

	group := &Group{
		file: file,
		name: header.Name,
	}

	// Загружаем детей только для групп
	if header.Type == core.ObjectTypeGroup {
		for _, msg := range header.Messages {
			switch msg.Type {
			case core.MsgSymbolTable:
				group.symbolTable, err = structures.ParseSymbolTable(r, msg.Offset, sb)
				if err != nil {
					return nil, utils.WrapError("symbol table parse failed", err)
				}
			}
		}

		if group.symbolTable != nil {
			if err := group.loadChildren(); err != nil {
				return nil, utils.WrapError("load children failed", err)
			}
		}
	}

	return group, nil
}

func (g *Group) loadChildren() error {
	heap, err := structures.LoadLocalHeap(g.file.osFile, g.symbolTable.HeapAddress, g.file.sb)
	if err != nil {
		return utils.WrapError("local heap load failed", err)
	}

	entries, err := structures.ReadBTreeEntries(g.file.osFile, g.symbolTable.BTreeAddress, g.file.sb)
	if err != nil {
		return utils.WrapError("B-tree read failed", err)
	}

	for _, entry := range entries {
		linkName, err := heap.GetString(entry.LinkNameOffset)
		if err != nil {
			return utils.WrapError("link name read failed", err)
		}

		child, err := loadObject(g.file, entry.ObjectAddress, linkName)
		if err != nil {
			return utils.WrapError("child load failed", err)
		}

		g.children = append(g.children, child)
	}

	return nil
}

func loadObject(file *File, address uint64, name string) (Object, error) {
	header, err := core.ReadObjectHeader(file.osFile, address, file.sb)
	if err != nil {
		return nil, err
	}

	switch header.Type {
	case core.ObjectTypeGroup:
		return loadGroup(file, address)
	case core.ObjectTypeDataset:
		return &Dataset{
			file: file,
			name: name,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported object type: %d", header.Type)
	}
}
