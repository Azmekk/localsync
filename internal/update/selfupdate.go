package update

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SelfUpdate downloads the latest release and replaces both localsync and syncclient binaries.
func SelfUpdate(callingBinary string) error {
	fmt.Println("Checking for latest release...")

	info, err := CheckLatest()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if Version != "dev" && CompareVersions(Version, info.TagName) >= 0 {
		fmt.Printf("Already up to date (%s)\n", Version)
		return nil
	}

	fmt.Printf("Updating to %s...\n", info.TagName)

	// Determine the directory where the calling binary lives
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	selfPath, err = filepath.EvalSymlinks(selfPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable symlinks: %w", err)
	}
	binDir := filepath.Dir(selfPath)

	binaries := []string{"localsync", "syncclient"}
	for _, binName := range binaries {
		asset := findAsset(info, binName)
		if asset == nil {
			fmt.Printf("warning: no release asset found for %s (%s/%s), skipping\n",
				binName, runtime.GOOS, runtime.GOARCH)
			continue
		}

		targetPath := filepath.Join(binDir, binName)
		if runtime.GOOS == "windows" {
			targetPath += ".exe"
		}

		// If the target doesn't exist at the expected path, try LookPath as fallback
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			lookName := binName
			if runtime.GOOS == "windows" {
				lookName += ".exe"
			}
			if found, lookErr := exec.LookPath(lookName); lookErr == nil {
				targetPath = found
			}
		}

		fmt.Printf("Downloading %s...\n", asset.Name)
		if err := downloadAndReplace(asset.BrowserDownloadURL, targetPath); err != nil {
			return fmt.Errorf("failed to update %s: %w", binName, err)
		}
		fmt.Printf("Updated %s\n", binName)
	}

	fmt.Println("Update complete!")
	return nil
}

func findAsset(info *ReleaseInfo, binaryName string) *Asset {
	expected := assetName(binaryName)
	for i := range info.Assets {
		if info.Assets[i].Name == expected {
			return &info.Assets[i]
		}
	}
	return nil
}

func assetName(binaryName string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("%s-%s-%s%s", binaryName, runtime.GOOS, runtime.GOARCH, ext)
}

func downloadAndReplace(url, targetPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Write to a temp file in the same directory as the target
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	tmpFile, err := os.CreateTemp(dir, strings.TrimSuffix(base, filepath.Ext(base))+"-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // clean up on failure

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	if err := replaceBinary(tmpPath, targetPath); err != nil {
		return err
	}
	return nil
}
