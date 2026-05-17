package vm

import (
	"fmt"
	"io"
	"os"
)

const sparseBlockSize = 512 * 1024 // 512 KiB read buffer

// sparseCopyFallback is a portable sparse-aware copy that reads data extents
// and recreates holes by seeking the destination past zero regions.
//
// It works on any OS/filesystem: on Linux it uses SEEK_HOLE/SEEK_DATA when
// available (via platform files); on macOS it falls back here only when
// clonefile fails.  Zero-filled blocks are detected and skipped; the
// destination file is extended with a seek+truncate at the end so its logical
// size matches the source.
func sparseCopyFallback(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
		if err != nil {
			_ = os.Remove(dst)
		}
	}()

	buf := make([]byte, sparseBlockSize)
	var srcOff int64

	for srcOff < size {
		n := int64(sparseBlockSize)
		if srcOff+n > size {
			n = size - srcOff
		}

		nr, readErr := io.ReadFull(in, buf[:n])
		if nr > 0 {
			data := buf[:nr]
			if isZero(data) {
				// Skip: seek destination forward; don't write zeros.
				srcOff += int64(nr)
				continue
			}
			if _, werr := out.WriteAt(data, srcOff); werr != nil {
				err = fmt.Errorf("writing at offset %d: %w", srcOff, werr)
				return err
			}
		}
		srcOff += int64(nr)
		if readErr != nil {
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			err = readErr
			return err
		}
	}

	// Extend the file to full logical size without allocating disk blocks.
	if terr := out.Truncate(size); terr != nil {
		err = fmt.Errorf("truncating to logical size: %w", terr)
		return err
	}
	return nil
}

// isZero returns true if every byte in b is zero.
func isZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
