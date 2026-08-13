// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Options describes a requested user-level binary installation. SourceDir must
// contain the two release binaries. DryRun performs all validation but writes
// neither binaries nor a receipt.
type Options struct {
	Environment Environment
	SourceDir   string
	InstallRoot string
	DryRun      bool
	Force       bool
	// DeferReceipt lets the caller finish a platform PATH update before it
	// records a successful, uninstallable installation.
	DeferReceipt bool
}

type Result struct {
	Layout  Layout
	Receipt Receipt
}

// Install copies both release binaries atomically and records their hashes.
// PATH mutation intentionally remains outside this function: callers need a
// platform-specific adapter, while this core is safe to test in a temp dir.
func Install(options Options) (Result, error) {
	layout, err := DefaultLayout(options.Environment)
	if err != nil {
		return Result{}, err
	}
	if options.InstallRoot != "" {
		layout.InstallRoot = filepath.ToSlash(options.InstallRoot)
		layout.ReceiptPath = filepath.ToSlash(filepath.Join(filepath.Dir(layout.ReceiptPath), "install-receipt.json"))
		ext := ""
		if options.Environment.GOOS == "windows" {
			ext = ".exe"
		}
		layout.Binaries = []BinaryTarget{{Name: "ai-prov", Path: pathJoin(layout.InstallRoot, "ai-prov"+ext)}, {Name: "ai-prov-mcp", Path: pathJoin(layout.InstallRoot, "ai-prov-mcp"+ext)}}
		if err := layout.Validate(); err != nil {
			return Result{}, err
		}
	}
	receipt := Receipt{SchemaVersion: ReceiptSchemaVersion, Platform: options.Environment.GOOS, InstallRoot: layout.InstallRoot, PATH: PATHRecord{Method: "pending", Entry: layout.InstallRoot}}
	for _, target := range layout.Binaries {
		source := filepath.Join(options.SourceDir, filepath.Base(target.Path))
		hash, err := fileHash(source)
		if err != nil {
			return Result{}, fmt.Errorf("read release binary %s: %w", target.Name, err)
		}
		receipt.Binaries = append(receipt.Binaries, InstalledBin{Name: target.Name, Path: target.Path, SHA256: hash})
	}
	if options.DryRun {
		return Result{Layout: layout, Receipt: receipt}, nil
	}
	if err := os.MkdirAll(filepath.FromSlash(layout.InstallRoot), 0o755); err != nil {
		return Result{}, fmt.Errorf("create install root: %w", err)
	}
	for _, target := range layout.Binaries {
		source := filepath.Join(options.SourceDir, filepath.Base(target.Path))
		if err := copyAtomic(source, filepath.FromSlash(target.Path), options.Force); err != nil {
			return Result{}, err
		}
	}
	if !options.DeferReceipt {
		if err := WriteReceipt(filepath.FromSlash(layout.ReceiptPath), receipt); err != nil {
			return Result{}, err
		}
	}
	return Result{Layout: layout, Receipt: receipt}, nil
}

func pathJoin(parts ...string) string { return filepath.ToSlash(filepath.Join(parts...)) }

func fileHash(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyAtomic(source, target string, force bool) error {
	if existing, err := fileHash(target); err == nil {
		sourceHash, sourceErr := fileHash(source)
		if sourceErr != nil {
			return sourceErr
		}
		if existing == sourceHash {
			return nil
		}
		if !force {
			return fmt.Errorf("install target already differs: %s (pass --force to replace)", target)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), ".ai-prov-install-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = io.Copy(temp, in); err == nil {
		err = temp.Chmod(0o755)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tempName, target)
}

// WriteReceipt persists a previously validated, complete installation record.
func WriteReceipt(name string, receipt Receipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(name), ".install-receipt-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(append(data, '\n')); err == nil {
		err = temp.Chmod(0o600)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tempName, name)
}

// ReadReceipt validates the exact persisted installation authority before an
// uninstall uses it. Callers must treat a missing receipt as a no-op.
func ReadReceipt(name string) (Receipt, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, fmt.Errorf("decode installation receipt: %w", err)
	}
	if err := receipt.Validate(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

type UninstallResult struct{ Removed, Preserved []string }

// MatchingBinaryPaths returns only receipt-owned binaries whose current bytes
// match their recorded hash. Deferred Windows removal consumes this explicit,
// validated list and never discovers files by name.
func MatchingBinaryPaths(receipt Receipt) ([]string, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	var paths []string
	for _, binary := range receipt.Binaries {
		actual, err := fileHash(filepath.FromSlash(binary.Path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if actual == binary.SHA256 {
			paths = append(paths, binary.Path)
		}
	}
	return paths, nil
}

// Uninstall removes only receipt-listed binaries whose bytes still match the
// recorded hash. It never searches by filename or removes project data.
func Uninstall(receipt Receipt, dryRun bool) (UninstallResult, error) {
	if err := receipt.Validate(); err != nil {
		return UninstallResult{}, err
	}
	result := UninstallResult{}
	for _, binary := range receipt.Binaries {
		actual, err := fileHash(filepath.FromSlash(binary.Path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return result, err
		}
		if actual != binary.SHA256 {
			result.Preserved = append(result.Preserved, binary.Path)
			continue
		}
		result.Removed = append(result.Removed, binary.Path)
		if !dryRun {
			if err := os.Remove(filepath.FromSlash(binary.Path)); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
