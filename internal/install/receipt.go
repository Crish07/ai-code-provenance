// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package install

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

const ReceiptSchemaVersion = 1

var ErrInvalidReceipt = errors.New("invalid installation receipt")

// Environment contains only the host facts needed to calculate a user-level
// installation. Keeping it injectable makes platform decisions testable
// without reading a developer machine's environment.
type Environment struct {
	GOOS         string
	Home         string
	LocalAppData string
}

// Layout is the deterministic set of user-owned paths for an installation.
// Paths use slash separators intentionally: Go and Windows APIs accept them,
// and the receipt remains stable across a release built on a different host.
type Layout struct {
	InstallRoot string
	ReceiptPath string
	Binaries    []BinaryTarget
}

type BinaryTarget struct {
	Name string
	Path string
}

// DefaultLayout selects a per-user install root. Unix uses ~/.local/bin;
// Windows uses %LOCALAPPDATA%/Programs/ai-prov. No system-level directory is
// ever selected.
func DefaultLayout(env Environment) (Layout, error) {
	if env.Home == "" {
		return Layout{}, fmt.Errorf("%w: home directory is required", ErrInvalidReceipt)
	}
	var root, receipt string
	switch env.GOOS {
	case "darwin", "linux":
		root = path.Join(env.Home, ".local", "bin")
		receipt = path.Join(env.Home, ".local", "share", "ai-prov", "install-receipt.json")
	case "windows":
		if env.LocalAppData == "" {
			return Layout{}, fmt.Errorf("%w: LOCALAPPDATA is required on windows", ErrInvalidReceipt)
		}
		localAppData := windowsReceiptPath(env.LocalAppData)
		root = path.Join(localAppData, "Programs", "ai-prov")
		receipt = path.Join(localAppData, "ai-prov", "install-receipt.json")
	default:
		return Layout{}, fmt.Errorf("%w: unsupported platform %q", ErrInvalidReceipt, env.GOOS)
	}
	ext := ""
	if env.GOOS == "windows" {
		ext = ".exe"
	}
	layout := Layout{InstallRoot: root, ReceiptPath: receipt, Binaries: []BinaryTarget{
		{Name: "ai-prov", Path: path.Join(root, "ai-prov"+ext)},
		{Name: "ai-prov-mcp", Path: path.Join(root, "ai-prov-mcp"+ext)},
	}}
	return layout, layout.Validate()
}

// windowsReceiptPath canonicalizes the environment and flag paths that
// Windows exposes with backslashes before they are persisted in a portable
// receipt. Go's path package intentionally does not treat backslashes as path
// separators, so this conversion must happen before path.Join and validation.
func windowsReceiptPath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

// Receipt is the only authority a future uninstall may use to identify files
// it owns. It is intentionally explicit rather than inferring paths by name.
type Receipt struct {
	SchemaVersion int            `json:"schema_version"`
	Platform      string         `json:"platform"`
	InstallRoot   string         `json:"install_root"`
	Binaries      []InstalledBin `json:"binaries"`
	PATH          PATHRecord     `json:"path"`
}

type InstalledBin struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// PATHRecord describes only a change made by ai-prov. A later uninstall must
// remove this exact record, never reconstruct or rewrite unrelated PATH data.
type PATHRecord struct {
	Method    string `json:"method"`
	Entry     string `json:"entry"`
	BeginMark string `json:"begin_mark,omitempty"`
	EndMark   string `json:"end_mark,omitempty"`
}

func (l Layout) Validate() error {
	if !isAbsoluteClean(l.InstallRoot) || !isAbsoluteClean(l.ReceiptPath) {
		return fmt.Errorf("%w: layout paths must be absolute and clean", ErrInvalidReceipt)
	}
	if len(l.Binaries) != 2 {
		return fmt.Errorf("%w: layout must contain exactly two binaries", ErrInvalidReceipt)
	}
	seen := make(map[string]bool, len(l.Binaries))
	for _, binary := range l.Binaries {
		if binary.Name == "" || seen[binary.Name] || !within(l.InstallRoot, binary.Path) {
			return fmt.Errorf("%w: invalid binary target %q", ErrInvalidReceipt, binary.Name)
		}
		seen[binary.Name] = true
	}
	return nil
}

// Validate rejects receipts that could make an uninstall act outside its
// recorded user-owned root. File hashes are lower-case SHA-256 hex strings.
func (r Receipt) Validate() error {
	if r.SchemaVersion != ReceiptSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %d", ErrInvalidReceipt, r.SchemaVersion)
	}
	if r.Platform != "darwin" && r.Platform != "linux" && r.Platform != "windows" {
		return fmt.Errorf("%w: unsupported platform %q", ErrInvalidReceipt, r.Platform)
	}
	if !isAbsoluteClean(r.InstallRoot) || len(r.Binaries) != 2 {
		return fmt.Errorf("%w: invalid install root or binary count", ErrInvalidReceipt)
	}
	seen := map[string]bool{}
	for _, binary := range r.Binaries {
		if binary.Name == "" || seen[binary.Name] || !within(r.InstallRoot, binary.Path) || !isSHA256(binary.SHA256) {
			return fmt.Errorf("%w: invalid binary %q", ErrInvalidReceipt, binary.Name)
		}
		seen[binary.Name] = true
	}
	if r.PATH.Method == "" || !within(r.InstallRoot, r.PATH.Entry) {
		return fmt.Errorf("%w: invalid PATH record", ErrInvalidReceipt)
	}
	return nil
}

func isAbsoluteClean(p string) bool {
	if path.Clean(p) != p {
		return false
	}
	// path.IsAbs is host-dependent. Receipts may be validated on a different
	// host than the one that created them, so recognise a normalized Windows
	// drive path as well as a Unix absolute path.
	if path.IsAbs(p) {
		return true
	}
	return len(p) >= 3 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' && p[2] == '/'
}

func within(root, candidate string) bool {
	if !isAbsoluteClean(root) || !isAbsoluteClean(candidate) {
		return false
	}
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}

func isSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
