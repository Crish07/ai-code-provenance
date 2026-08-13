// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ai-prov/internal/workspace"
)

type File struct{ Path, Hash string }
type Manifest struct {
	Version      int
	ID           string
	Files        []File
	SkippedCount int
}
type GCReport struct {
	SessionIDs    []string
	ObjectHashes  []string
	SnapshotBytes int64
	ObjectBytes   int64
}

// QuotaExceededError reports a rejected snapshot before it writes a manifest,
// object, or session row. Limit is expressed in bytes; Existing and Required
// are respectively the current object-store size and this snapshot's unique
// additional object bytes.
type QuotaExceededError struct {
	Limit, Existing, Required int64
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("snapshot quota exceeded: %d existing bytes + %d required bytes exceeds %d bytes", e.Existing, e.Required, e.Limit)
}

func Create(root, id string, max int64) (Manifest, error) {
	return CreateWithQuota(root, id, max, 0)
}

// CreateWithQuota writes a v2 manifest only after it has scanned and
// normalized every tracked file and proved that the object store will fit
// within maxSnapshotBytes. A zero limit disables the quota for direct callers
// that have not opted into a project-level capacity policy.
func CreateWithQuota(root, id string, maxFileBytes, maxSnapshotBytes int64) (Manifest, error) {
	files, skipped, err := workspace.Scan(root, maxFileBytes)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{Version: 2, ID: id, SkippedCount: len(skipped)}
	type object struct {
		hash string
		data []byte
	}
	objects := make(map[string]object, len(files))
	for _, f := range files {
		data := Normalize(f.Data)
		hash := Hash(data)
		m.Files = append(m.Files, File{f.Path, hash})
		objects[hash] = object{hash: hash, data: data}
	}
	if maxSnapshotBytes > 0 {
		existing, present, err := objectStoreUsage(root)
		if err != nil {
			return Manifest{}, err
		}
		var required int64
		for hash, obj := range objects {
			if !present[hash] {
				required += int64(len(obj.data))
			}
		}
		if existing+required > maxSnapshotBytes {
			return Manifest{}, &QuotaExceededError{Limit: maxSnapshotBytes, Existing: existing, Required: required}
		}
	}
	dir := filepath.Join(root, ".ai-provenance", "snapshots", id)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	for _, obj := range objects {
		if err = writeObject(root, obj.hash, obj.data); err != nil {
			return Manifest{}, err
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, err
	}
	tmp, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return Manifest{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return Manifest{}, err
	}
	if err = tmp.Close(); err != nil {
		return Manifest{}, err
	}
	if err = os.Rename(tmpName, filepath.Join(dir, "manifest.json")); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// objectStoreUsage returns the full byte size of regular object-store entries
// and hashes already present as regular files. Counting all files keeps a
// manually introduced orphan from silently bypassing the configured limit.
func objectStoreUsage(root string) (int64, map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".ai-provenance", "objects"))
	if os.IsNotExist(err) {
		return 0, map[string]bool{}, nil
	}
	if err != nil {
		return 0, nil, err
	}
	present := make(map[string]bool, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, nil, err
		}
		total += info.Size()
		name := entry.Name()
		if len(name) == sha256.Size*2 {
			if _, err := hex.DecodeString(name); err == nil {
				present[name] = true
			}
		}
	}
	return total, present, nil
}

// Hash returns the SHA-256 of canonical snapshot content.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Matches reports whether current canonical content is identical to the
// manifest entry. Finish uses it as a fast path before allocating a line diff.
func Matches(data []byte, expectedHash string) bool {
	return Hash(data) == expectedHash
}

// Normalize produces the canonical text representation used on both sides of
// a provenance diff. Keeping this operation shared prevents an unchanged CRLF
// file from differing from its LF-normalized snapshot at session finish.
func Normalize(data []byte) []byte {
	if !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
}

func Verify(root string, m Manifest) bool {
	for _, f := range m.Files {
		b, e := ReadFile(root, m, f)
		if e != nil {
			return false
		}
		if !Matches(b, f.Hash) {
			return false
		}
	}
	return true
}

// ReadFile reads one baseline file from the content-addressed v2 object store
// or from the v1 per-session directory. It validates the manifest hash before
// returning bytes so corrupted objects cannot silently change attribution.
func ReadFile(root string, m Manifest, f File) ([]byte, error) {
	path := filepath.Join(root, ".ai-provenance", "snapshots", m.ID, filepath.FromSlash(f.Path))
	if m.Version >= 2 {
		path = objectPath(root, f.Hash)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if Hash(b) != f.Hash {
		return nil, fmt.Errorf("snapshot hash mismatch for %s", f.Path)
	}
	return b, nil
}

func objectPath(root, hash string) string {
	return filepath.Join(root, ".ai-provenance", "objects", hash)
}
func writeObject(root, hash string, data []byte) error {
	if Hash(data) != hash {
		return fmt.Errorf("object hash mismatch")
	}
	dir := filepath.Dir(objectPath(root, hash))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := objectPath(root, hash)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".object-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return nil
}
func Read(root, id string) (Manifest, error) {
	b, e := os.ReadFile(filepath.Join(root, ".ai-provenance", "snapshots", id, "manifest.json"))
	if e != nil {
		return Manifest{}, e
	}
	var m Manifest
	e = json.Unmarshal(b, &m)
	return m, e
}

// GCDryRun reports terminal snapshot directories and v2 objects that become
// unreachable if sessionIDs are removed. It never mutates the filesystem.
func GCDryRun(root string, sessionIDs []string) (GCReport, error) {
	candidates := map[string]struct{}{}
	for _, id := range sessionIDs {
		candidates[id] = struct{}{}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".ai-provenance", "snapshots"))
	if err != nil {
		return GCReport{}, err
	}
	reachable := map[string]struct{}{}
	var report GCReport
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		m, err := Read(root, id)
		if err != nil {
			return GCReport{}, err
		}
		if _, remove := candidates[id]; remove {
			report.SessionIDs = append(report.SessionIDs, id)
			size, err := dirSize(filepath.Join(root, ".ai-provenance", "snapshots", id))
			if err != nil {
				return GCReport{}, err
			}
			report.SnapshotBytes += size
			continue
		}
		if m.Version >= 2 {
			for _, f := range m.Files {
				reachable[f.Hash] = struct{}{}
			}
		}
	}
	objects, err := os.ReadDir(filepath.Join(root, ".ai-provenance", "objects"))
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return GCReport{}, err
	}
	for _, entry := range objects {
		if entry.IsDir() {
			continue
		}
		if _, ok := reachable[entry.Name()]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return GCReport{}, err
		}
		report.ObjectHashes = append(report.ObjectHashes, entry.Name())
		report.ObjectBytes += info.Size()
	}
	sort.Strings(report.SessionIDs)
	sort.Strings(report.ObjectHashes)
	return report, nil
}

// GCApply recomputes a dry-run plan and deletes only its validated terminal
// snapshot directories and unreachable objects.
func GCApply(root string, sessionIDs []string) (GCReport, error) {
	report, err := GCDryRun(root, sessionIDs)
	if err != nil {
		return GCReport{}, err
	}
	for _, id := range report.SessionIDs {
		if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
			return GCReport{}, fmt.Errorf("unsafe snapshot id %q", id)
		}
		if err := os.RemoveAll(filepath.Join(root, ".ai-provenance", "snapshots", id)); err != nil {
			return GCReport{}, err
		}
	}
	for _, hash := range report.ObjectHashes {
		if len(hash) != 64 {
			return GCReport{}, fmt.Errorf("unsafe object hash %q", hash)
		}
		if err := os.Remove(objectPath(root, hash)); err != nil && !os.IsNotExist(err) {
			return GCReport{}, err
		}
	}
	return report, nil
}
func dirSize(path string) (int64, error) {
	var n int64
	err := filepath.WalkDir(path, func(_ string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() {
			i, err := e.Info()
			if err != nil {
				return err
			}
			n += i.Size()
		}
		return nil
	})
	return n, err
}
