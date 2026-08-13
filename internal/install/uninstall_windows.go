// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

//go:build windows

package install

import (
	"fmt"
	"os/exec"
	"strings"
)

// ScheduleWindowsRemoval starts a detached helper after receipt/hash
// validation. The helper receives only explicit paths and cannot recurse.
func ScheduleWindowsRemoval(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		if !isAbsoluteClean(p) || strings.ContainsAny(p, "&|<>^\"") {
			return fmt.Errorf("%w: unsafe deferred delete path", ErrInvalidReceipt)
		}
		quoted = append(quoted, `del /f /q "`+strings.ReplaceAll(p, "/", `\`)+`"`)
	}
	return exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 2 > nul & "+strings.Join(quoted, " & ")).Start()
}
