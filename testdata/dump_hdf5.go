package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: dump_hdf5 <file.h5>")
		return
	}

	file := os.Args[1]
	f, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Читаем первые 512 байт файла
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("First %d bytes of %s:\n", n, file)
	for i := 0; i < n; i += 16 {
		end := i + 16
		if end > n {
			end = n
		}
		chunk := buf[i:end]

		// Шестнадцатеричный дамп
		fmt.Printf("%08x: ", i)
		for j := 0; j < 16; j++ {
			if j < len(chunk) {
				fmt.Printf("%02x ", chunk[j])
			} else {
				fmt.Print("   ")
			}
			if j == 7 {
				fmt.Print(" ")
			}
		}
		fmt.Print(" |")

		// ASCII представление
		for _, b := range chunk {
			if b >= 32 && b <= 126 {
				fmt.Printf("%c", b)
			} else {
				fmt.Print(".")
			}
		}
		fmt.Println("|")
	}
}
