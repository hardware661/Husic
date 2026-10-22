package netease

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Vector produced independently with `openssl enc -aes-128-ecb`.
func TestEapiFormMatchesReferenceCipher(t *testing.T) {
	form, err := eapiForm("/api/song/enhance/player/url/v1", map[string]any{"ids": "[1]", "level": "standard"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "FA90B329E9614F79E79598F37DC2EDB487F00D1BC4C9B24CD57E6C318B9073569338432CD7D98D1A3626E997A2C53121F2652556B9CAA141000D39A0C1D1AE12317F2B9E9ED1864F44D1D6A14668C078B8932DE8811372CC0A101C6E1850331A25659182F2FF2A0A1F2071E68CFF3C7CD5625E2FACE01B3EB523FBCBEF2F7274"
	if got := form.Get("params"); got != want {
		t.Fatalf("eapi params = %s", got)
	}
}

func cbcDecrypt(t *testing.T, sealed, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil || len(sealed)%aes.BlockSize != 0 {
		t.Fatal("bad cipher input")
	}
	plain := append([]byte(nil), sealed...)
	cipher.NewCBCDecrypter(block, []byte(weapiIV)).CryptBlocks(plain, plain)
	return plain[:len(plain)-int(plain[len(plain)-1])]
}

func TestWeapiFormRoundTripsPayloadAndCSRF(t *testing.T) {
	key := []byte("abcdefghijklmnop")
	form, err := weapiFormWithKey(map[string]any{"type": 1, "csrf_token": "ignored"}, "token", key)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := base64.StdEncoding.DecodeString(form.Get("params"))
	if err != nil {
		t.Fatal(err)
	}
	inner, err := base64.StdEncoding.DecodeString(string(cbcDecrypt(t, outer, key)))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(cbcDecrypt(t, inner, []byte(weapiPresetKey)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != float64(1) || payload["csrf_token"] != "token" {
		t.Fatalf("payload = %v", payload)
	}
	if n := len(form.Get("encSecKey")); n != 256 {
		t.Fatalf("encSecKey width = %d", n)
	}
}
