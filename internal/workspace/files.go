package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type File struct {
	Path string
	Data []byte
}
type Skipped struct{ Path, Reason string }

// Scan returns stable, project-relative UTF-8 text files.
func Scan(root string, maxBytes int64) ([]File, []Skipped, error) {
	var files []File
	var skipped []Skipped
	ignored := loadIgnores(root)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".gitignore" || rel == ".ai-provenanceignore" {
			return nil
		}
		if entry.IsDir() {
			if hiddenDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			skipped = append(skipped, Skipped{rel, "symlink"})
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if ignored(rel) {
			skipped = append(skipped, Skipped{rel, "ignored"})
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxBytes {
			skipped = append(skipped, Skipped{rel, "too_large"})
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			skipped = append(skipped, Skipped{rel, "non_utf8_or_binary"})
			return nil
		}
		files = append(files, File{rel, data})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan workspace: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return files, skipped, nil
}
func loadIgnores(root string) func(string) bool {
	var p []string
	for _, n := range []string{".gitignore", ".ai-provenanceignore"} {
		if b, e := os.ReadFile(filepath.Join(root, n)); e == nil {
			for _, s := range strings.Fields(string(b)) {
				if !strings.HasPrefix(s, "#") {
					p = append(p, s)
				}
			}
		}
	}
	return func(path string) bool {
		for _, s := range p {
			if ok, _ := filepath.Match(s, path); ok {
				return true
			}
			if ok, _ := filepath.Match(s, filepath.Base(path)); ok {
				return true
			}
		}
		return false
	}
}
func hiddenDir(path string) bool {
	first := strings.Split(path, "/")[0]
	switch first {
	case ".git", ".ai-provenance", "node_modules", "vendor", "dist", "build":
		return true
	}
	return false
}
