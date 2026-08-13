// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

//go:build !windows

package install

import "fmt"

// UpdateWindowsUserPATH cannot run in a non-Windows binary. Keeping a stub
// makes adapter selection explicit and prevents accidental platform spoofing.
func UpdateWindowsUserPATH(string) error {
	return fmt.Errorf("Windows user PATH updates require Windows")
}

func RemoveWindowsUserPATH(string) error {
	return fmt.Errorf("Windows user PATH updates require Windows")
}
