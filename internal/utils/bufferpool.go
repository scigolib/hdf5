package utils

import "sync"

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 4096)
	},
}

// GetBuffer returns a byte slice from the pool
func GetBuffer(size int) []byte {
	buf := bufferPool.Get().([]byte)
	if cap(buf) < size {
		return make([]byte, size, size*2) // Увеличиваем capacity
	}
	return buf[:size]
}

// ReleaseBuffer returns a buffer to the pool
func ReleaseBuffer(buf []byte) {
	bufferPool.Put(buf[:0])
}
