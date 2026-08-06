package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ai-prov/internal/workspace"
)

type File struct{ Path, Hash string }
type Manifest struct {
	ID           string
	Files        []File
	SkippedCount int
}

func Create(root, id string, max int64) (Manifest, error) {
	files, skipped, err := workspace.Scan(root, max)
	if err != nil {
		return Manifest{}, err
	}
	dir := filepath.Join(root, ".ai-provenance", "snapshots", id)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	m := Manifest{ID: id, SkippedCount: len(skipped)}
	for _, f := range files {
		data := []byte(strings.ReplaceAll(string(f.Data), "\r\n", "\n"))
		h := sha256.Sum256(data)
		m.Files = append(m.Files, File{f.Path, hex.EncodeToString(h[:])})
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Manifest{}, err
		}
		if err = os.WriteFile(p, data, 0o644); err != nil {
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
func Verify(root string, m Manifest) bool {
	for _, f := range m.Files {
		b, e := os.ReadFile(filepath.Join(root, ".ai-provenance", "snapshots", m.ID, filepath.FromSlash(f.Path)))
		if e != nil {
			return false
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != f.Hash {
			return false
		}
	}
	return true
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
