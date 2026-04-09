package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localsync/localsync/internal/model"
)

// videoExtensions lists recognized video file extensions.
var videoExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".webm": true,
	".avi":  true,
	".mov":  true,
	".ts":   true,
	".flv":  true,
}

// VariantDir returns the .localsync/ directory path next to the given video file.
func VariantDir(videoPath string) string {
	return filepath.Join(filepath.Dir(videoPath), ".localsync")
}

// ScanVariants looks for pre-compressed video files in the .localsync/ folder
// next to the given video file. Returns an empty slice if the folder doesn't exist.
func ScanVariants(videoPath string) []model.Variant {
	dir := VariantDir(videoPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var variants []model.Variant
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !videoExtensions[ext] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		variants = append(variants, model.Variant{
			Name:     name,
			Filename: entry.Name(),
			Size:     info.Size(),
		})
	}

	sort.Slice(variants, func(i, j int) bool {
		return variants[i].Name < variants[j].Name
	})
	return variants
}

// CreateMediaFolder creates the .localsync/ directory next to the given video file
// and prints instructions for the user.
func CreateMediaFolder(videoPath string) error {
	dir := VariantDir(videoPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create media folder: %w", err)
	}
	fmt.Printf("Created media folder: %s\n", dir)
	fmt.Println()
	fmt.Println("Place pre-compressed video variants in this folder.")
	fmt.Println("Use any naming convention you like, e.g.:")
	fmt.Println("  720p_low.mkv")
	fmt.Println("  720p_high.mp4")
	fmt.Println("  480p.webm")
	fmt.Println()
	fmt.Println("Clients will be able to choose from these variants when connecting.")
	return nil
}
