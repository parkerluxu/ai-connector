//go:build windows

package connectorconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveProtectsPrivateKeyWithDPAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connector.json")
	config := Config{APIURL: DefaultAPIURL, WSURL: DefaultWSURL}
	_, privateKey, err := config.EnsureIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), config.PrivateKey) {
		t.Fatal("configuration contains the plaintext device key")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PrivateKey != "" || loaded.ProtectedPrivateKey == "" {
		t.Fatalf("unexpected stored key fields: %#v", loaded)
	}
	_, restored, err := loaded.EnsureIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(privateKey) {
		t.Fatal("DPAPI did not restore the original device key")
	}
}
