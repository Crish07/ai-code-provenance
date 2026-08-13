// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

//go:build windows

package install

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// UpdateWindowsUserPATH updates only the current user's PATH and broadcasts
// the standard environment-change notification for future processes.
func UpdateWindowsUserPATH(entry string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open current-user environment: %w", err)
	}
	defer key.Close()
	current, _, err := key.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("read current-user PATH: %w", err)
	}
	updated, err := WindowsUserPATH(current, entry)
	if err != nil {
		return err
	}
	if err := key.SetStringValue("Path", updated); err != nil {
		return fmt.Errorf("write current-user PATH: %w", err)
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	send := user32.NewProc("SendMessageTimeoutW")
	environment := windows.StringToUTF16Ptr("Environment")
	const (
		hwndBroadcast   = uintptr(0xffff)
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	var ignored uintptr
	if r, _, callErr := send.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&ignored))); r == 0 && callErr != windows.ERROR_SUCCESS {
		return fmt.Errorf("broadcast PATH change: %w", callErr)
	}
	return nil
}

func RemoveWindowsUserPATH(entry string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open current-user environment: %w", err)
	}
	defer key.Close()
	current, _, err := key.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("read current-user PATH: %w", err)
	}
	updated, err := RemoveWindowsUserPATHValue(current, entry)
	if err != nil {
		return err
	}
	if err := key.SetStringValue("Path", updated); err != nil {
		return fmt.Errorf("write current-user PATH: %w", err)
	}
	return nil
}
