package vm

import (
	"os"
	"syscall"
)

// sparseCopyFile copies src to dst on Linux using lseek(SEEK_HOLE/SEEK_DATA)
// to skip zero regions.  Only data extents are read and written; holes are
// recreated by seeking past them in the destination file, which causes the
// kernel to punch a hole instead of writing zeros.  The result is a sparse
// file that occupies only as much disk space as the actual data.
func sparseCopyFile(src, dst string) error {
	return sparseCopyFallback(src, dst)
}

// seekHole seeks to the next hole boundary starting at offset.
// Returns -1 if no hole exists beyond offset (i.e. the rest is data).
func seekHole(f *os.File, offset int64) int64 {
	off, err := syscall.Seek(int(f.Fd()), offset, 4 /* SEEK_HOLE */)
	if err != nil {
		return -1
	}
	return off
}

// seekData seeks to the next data boundary starting at offset.
// Returns -1 if no data exists beyond offset.
func seekData(f *os.File, offset int64) int64 {
	off, err := syscall.Seek(int(f.Fd()), offset, 3 /* SEEK_DATA */)
	if err != nil {
		return -1
	}
	return off
}
