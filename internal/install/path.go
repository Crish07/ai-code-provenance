// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package install

import (
	"fmt"
	"strings"
)

const (
	ShellBeginMark = "# >>> ai-prov PATH >>>"
	ShellEndMark   = "# <<< ai-prov PATH <<<"
)

// ShellFragment adds an idempotent, delimited PATH entry to a shell profile.
// It only replaces its own marked block and leaves all other profile content
// byte-for-byte intact.
func ShellFragment(profile, entry string) (string, error) {
	if !isAbsoluteClean(entry) {
		return "", fmt.Errorf("%w: PATH entry must be absolute and clean", ErrInvalidReceipt)
	}
	start := strings.Index(profile, ShellBeginMark)
	end := strings.Index(profile, ShellEndMark)
	if (start < 0) != (end < 0) || (start >= 0 && end < start) {
		return "", fmt.Errorf("%w: malformed ai-prov PATH markers", ErrInvalidReceipt)
	}
	block := ShellBeginMark + "\nexport PATH=\"" + entry + ":$PATH\"\n" + ShellEndMark + "\n"
	if start >= 0 {
		end += len(ShellEndMark)
		if end < len(profile) && profile[end] == '\n' {
			end++
		}
		return profile[:start] + block + profile[end:], nil
	}
	if profile != "" && !strings.HasSuffix(profile, "\n") {
		profile += "\n"
	}
	return profile + block, nil
}

// WindowsUserPATH returns a semicolon-delimited user PATH with entry present
// once. Registry I/O is intentionally kept out of this deterministic helper.
func WindowsUserPATH(current, entry string) (string, error) {
	if !isAbsoluteClean(entry) {
		return "", fmt.Errorf("%w: PATH entry must be absolute and clean", ErrInvalidReceipt)
	}
	parts := strings.Split(current, ";")
	out := []string{entry}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.EqualFold(part, entry) {
			out = append(out, part)
		}
	}
	return strings.Join(out, ";"), nil
}

func RemoveWindowsUserPATHValue(current, entry string) (string, error) {
	if !isAbsoluteClean(entry) {
		return "", fmt.Errorf("%w: PATH entry must be absolute and clean", ErrInvalidReceipt)
	}
	parts := strings.Split(current, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.EqualFold(part, entry) {
			out = append(out, part)
		}
	}
	return strings.Join(out, ";"), nil
}

// RemoveShellFragment removes only the complete ai-prov marked PATH block.
func RemoveShellFragment(profile string) (string, error) {
	start, end := strings.Index(profile, ShellBeginMark), strings.Index(profile, ShellEndMark)
	if start < 0 && end < 0 {
		return profile, nil
	}
	if start < 0 || end < start {
		return "", fmt.Errorf("%w: malformed ai-prov PATH markers", ErrInvalidReceipt)
	}
	end += len(ShellEndMark)
	if end < len(profile) && profile[end] == '\n' {
		end++
	}
	return profile[:start] + profile[end:], nil
}
