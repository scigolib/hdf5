package hdf5

import (
	"errors"
	"os"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

type File struct {
	osFile *os.File
	sb     *core.Superblock
	root   *Group
}

func Open(filename string) (*File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, utils.WrapError("file open failed", err)
	}

	// Проверка сигнатуры перед чтением суперблока
	if !isHDF5File(f) {
		f.Close()
		return nil, errors.New("not an HDF5 file")
	}

	sb, err := core.ReadSuperblock(f)
	if err != nil {
		f.Close()
		return nil, utils.WrapError("superblock read failed", err)
	}

	file := &File{
		osFile: f,
		sb:     sb,
	}

	// Используем правильный адрес корневой группы
	rootAddress := sb.RootGroup
	/*if rootAddress == 0 {
		f.Close()
		return nil, errors.New("invalid root group address: 0")
	}*/

	file.root, err = loadGroup(file, rootAddress)
	if err != nil {
		f.Close()
		return nil, utils.WrapError("root group load failed", err)
	}

	return file, nil
}

// isHDF5File проверяет сигнатуру HDF5 файла
func isHDF5File(r utils.ReaderAt) bool {
	buf := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(buf)

	if _, err := r.ReadAt(buf, 0); err != nil {
		return false
	}
	return string(buf) == core.Signature
}

func (f *File) Close() error {
	return f.osFile.Close()
}

func (f *File) Root() *Group {
	return f.root
}

func (f *File) Walk(fn func(path string, obj Object)) {
	walkGroup(f.root, "/", fn)
}

func walkGroup(g *Group, currentPath string, fn func(string, Object)) {
	fn(currentPath, g)

	for _, child := range g.Children() {
		childPath := currentPath + child.Name()

		if childGroup, ok := child.(*Group); ok {
			walkGroup(childGroup, childPath+"/", fn)
		} else {
			fn(childPath, child)
		}
	}
}

func (f *File) SuperblockVersion() uint8 {
	return f.sb.Version
}
