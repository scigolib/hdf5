//go:build ignore
// +build ignore

package main

import (
	"github.com/scigolib/hdf5" // Используем нашу же библиотеку
)

func main() {
	// Создание простого файла
	f, _ := hdf5.Create("testdata/simple.h5")
	defer f.Close()

	data := []float32{1.0, 2.0, 3.0, 4.0}
	f.WriteDataset("/data", data)

	// Создание файла с составными типами
	f, _ = hdf5.Create("testdata/compound.h5")
	defer f.Close()

	type Particle struct {
		ID   uint64
		Pos  [3]float64
		Mass float32
	}
	particles := []Particle{
		{1, [3]float64{1.0, 2.0, 3.0}, 1.5},
		{2, [3]float64{4.0, 5.0, 6.0}, 2.5},
	}
	f.WriteDataset("/particles", particles)
}
