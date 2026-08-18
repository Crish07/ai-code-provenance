// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	type candidate struct{ path, rel string }
	var candidates []candidate
	ignored, err := loadIgnores(root)
	if err != nil {
		return nil, nil, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
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
		if rel == ".gitignore" {
			return nil
		}
		if entry.IsDir() {
			if protectedDir(rel) || (ignored.ignored(rel, true) && !ignored.mayIncludeDescendant(rel)) {
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
		if ignored.ignored(rel, false) {
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
		candidates = append(candidates, candidate{path, rel})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan workspace: %w", err)
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	jobs := make(chan candidate)
	type result struct {
		file    File
		skipped *Skipped
		err     error
	}
	results := make(chan result, len(candidates))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				data, e := os.ReadFile(c.path)
				if e != nil {
					results <- result{err: e}
					continue
				}
				if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
					s := Skipped{c.rel, "non_utf8_or_binary"}
					results <- result{skipped: &s}
					continue
				}
				results <- result{file: File{c.rel, data}}
			}
		}()
	}
	go func() {
		for _, c := range candidates {
			jobs <- c
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for r := range results {
		if r.err != nil {
			return nil, nil, fmt.Errorf("scan workspace: %w", r.err)
		}
		if r.skipped != nil {
			skipped = append(skipped, *r.skipped)
		} else {
			files = append(files, r.file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return files, skipped, nil
}

func loadIgnores(root string) (ignoreMatcher, error) {
	var combined strings.Builder
	for _, name := range []string{".gitignore", filepath.Join(".ai-provenance", ".ai-provenanceignore")} {
		contents, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return ignoreMatcher{}, fmt.Errorf("read ignore file %s: %w", name, err)
		}
		combined.Write(contents)
		combined.WriteByte('\n')
	}
	matcher, err := parseIgnore(combined.String())
	if err != nil {
		return ignoreMatcher{}, fmt.Errorf("parse provenance ignore rules: %w", err)
	}
	return matcher, nil
}

// protectedDir is the non-configurable scan boundary for repository metadata
// and ai-prov's own potentially source-bearing state. The other default
// exclusions are intentionally seeded into .ai-provenanceignore instead.
func protectedDir(path string) bool {
	first := strings.Split(path, "/")[0]
	switch first {
	case ".git", ".ai-provenance":
		return true
	}
	return false
}
