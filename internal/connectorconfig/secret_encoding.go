package connectorconfig

import (
	"encoding/base64"
	"fmt"
)

func encodeProtectedSecret(value []byte) (string, error) {
	if len(value) == 0 {
		return "", fmt.Errorf("protected device key is empty")
	}
	return base64.StdEncoding.EncodeToString(value), nil
}

func decodeProtectedSecret(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode protected device key: %w", err)
	}
	return decoded, nil
}
