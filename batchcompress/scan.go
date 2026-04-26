package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var videoExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".webm": true,
	".avi":  true,
	".mov":  true,
	".ts":   true,
	".flv":  true,
}

type SourceFile struct {
	Path string
	Size int64
}

func Scan(root string, recursive bool) ([]SourceFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if videoExtensions[strings.ToLower(filepath.Ext(root))] {
			return []SourceFile{{Path: root, Size: info.Size()}}, nil
		}
		return nil, nil
	}

	var files []SourceFile

	if recursive {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if filepath.Base(path) == ".localsync" {
					return filepath.SkipDir
				}
				return nil
			}
			if !videoExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			files = append(files, SourceFile{Path: path, Size: fi.Size()})
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !videoExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, SourceFile{
				Path: filepath.Join(root, e.Name()),
				Size: fi.Size(),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
