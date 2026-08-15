//go:build !windows

package connectorconfig

import "fmt"

// Unix configuration files are created with 0600 permissions. Native keychain
// integration is intentionally deferred until each supported desktop platform
// can be covered and tested consistently.
func usePlatformSecretProtection() bool { return false }

func protectPrivateKey(value string) (string, error) {
	return "", fmt.Errorf("platform secret protection is unavailable")
}
func unprotectPrivateKey(value string) (string, error) {
	return "", fmt.Errorf("platform secret protection is unavailable")
}
