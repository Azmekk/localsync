//go:build windows

package update

import "os"

func replaceBinary(srcPath, dstPath string) error {
	// Windows allows renaming a running executable but not overwriting it.
	oldPath := dstPath + ".old.exe"
	os.Remove(oldPath) // clean up any previous .old.exe

	// Rename the current binary out of the way (may fail if it doesn't exist yet, that's fine)
	_ = os.Rename(dstPath, oldPath)

	if err := os.Rename(srcPath, dstPath); err != nil {
		// Attempt to restore the old binary
		_ = os.Rename(oldPath, dstPath)
		return err
	}

	// Best-effort cleanup of old binary
	os.Remove(oldPath)
	return nil
}
