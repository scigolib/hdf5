package hdf5

import (
	"fmt"
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

	// Получаем размер файла
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, utils.WrapError("file stat failed", err)
	}
	fmt.Printf("File size: %d bytes\n", fi.Size())

	sb, err := core.ReadSuperblock(f)
	if err != nil {
		f.Close()
		return nil, utils.WrapError("superblock read failed", err)
	}

	file := &File{
		osFile: f,
		sb:     sb,
	}

	// Загрузка корневой группы
	file.root, err = loadGroup(file, sb.RootGroup)
	if err != nil {
		f.Close()
		return nil, utils.WrapError("root group load failed", err)
	}

	return file, nil
}

func (f *File) Close() error {
	return f.osFile.Close()
}

func (f *File) Root() *Group {
	return f.root
}

func (f *File) GetObject(path string) (Object, error) {
	return nil, nil
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

// SuperblockVersion возвращает версию суперблока
func (f *File) SuperblockVersion() uint8 {
	return f.sb.Version
}
