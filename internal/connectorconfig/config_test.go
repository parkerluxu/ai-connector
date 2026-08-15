package connectorconfig

import (
	"path/filepath"
	"testing"
)

func TestDefaultEndpointsAreOfficialService(t *testing.T) {
	config := Config{}
	config.FillDefaults()
	if config.APIURL != DefaultAPIURL || config.WSURL != DefaultWSURL {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestIdentityPersistsAcrossConfigReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connector.json")
	config := Config{APIURL: "http://localhost:3000", WSURL: "ws://localhost:3000/v1/connectors/ws"}
	publicKey, _, err := config.EnsureIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loadedPublic, _, err := loaded.EnsureIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if string(publicKey) != string(loadedPublic) || config.DeviceID != loaded.DeviceID || config.CredentialID != loaded.CredentialID {
		t.Fatal("connector identity changed after reload")
	}
}
