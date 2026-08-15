//go:build windows

package connectorconfig

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func usePlatformSecretProtection() bool { return true }

func protectPrivateKey(value string) (string, error) {
	input := []byte(value)
	if len(input) == 0 {
		return "", fmt.Errorf("device key is empty")
	}
	in := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return encodeProtectedSecret(unsafe.Slice(out.Data, out.Size))
}

func unprotectPrivateKey(value string) (string, error) {
	input, err := decodeProtectedSecret(value)
	if err != nil {
		return "", err
	}
	if len(input) == 0 {
		return "", fmt.Errorf("protected device key is empty")
	}
	in := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
