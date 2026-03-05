//go:build !windows

package update

import (
	"io"
	"os"
)

func replaceBinary(srcPath, dstPath string) error {
	if err := os.Chmod(srcPath, 0o755); err != nil {
		return err
	}

	// Try atomic rename first (works when src and dst are on the same filesystem)
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}

	// Fallback: copy for cross-device moves
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}
