// Package netease talks to NetEase Cloud Music with the same request
// envelopes its own web and app clients use. It does not touch media DRM.
package netease

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

const (
	weapiPresetKey = "0CoJUm6Qyw8W8jud" // gitleaks:allow — public constant from NetEase's own web script
	weapiIV        = "0102030405060708"
	eapiKey        = "e82ckenh8dichen8"
	weapiPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDgtQn2JZ34ZC28NWYpAUd98iZ37BUrX/aKzmFbt7clFSs6sXqHauqKWqdtLkF2KexO40H1YTX8z2lSgBBOAxLsvaklV8k4cBFK9snQXE9/DDaFt6Rr7iVZMldczhC0JNgTz+SHXT6CBHuX3e9SdB1Ua44oncaTWz7OBGLbCiK45wIDAQAB
-----END PUBLIC KEY-----`
)

var weapiRSA = mustPublicKey(weapiPublicKey)

func mustPublicKey(text string) *rsa.PublicKey {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		panic("netease: invalid web public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(err)
	}
	return key.(*rsa.PublicKey)
}

// weapiForm builds the web client's envelope: two AES-CBC layers, the second
// under a one-time key that is RSA-wrapped (textbook, reversed) into encSecKey.
func weapiForm(payload map[string]any, csrf string) (url.Values, error) {
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	return weapiFormWithKey(payload, csrf, key)
}

func weapiFormWithKey(payload map[string]any, csrf string, key []byte) (url.Values, error) {
	data := make(map[string]any, len(payload)+1)
	for name, value := range payload {
		data[name] = value
	}
	data["csrf_token"] = csrf
	plain, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	first := cbcEncrypt(plain, []byte(weapiPresetKey))
	second := cbcEncrypt([]byte(base64.StdEncoding.EncodeToString(first)), key)
	reversed := make([]byte, len(key))
	for i, c := range key {
		reversed[len(key)-1-i] = c
	}
	wrapped := new(big.Int).Exp(new(big.Int).SetBytes(reversed), big.NewInt(int64(weapiRSA.E)), weapiRSA.N)
	return url.Values{
		"params":    {base64.StdEncoding.EncodeToString(second)},
		"encSecKey": {fmt.Sprintf("%0256x", wrapped)},
	}, nil
}

// eapiForm builds the app client's envelope: AES-ECB over the API path, the
// JSON body and an MD5 check of both, upper-case hex.
func eapiForm(apiPath string, payload any) (url.Values, error) {
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	digest := md5.Sum([]byte("nobody" + apiPath + "use" + string(text) + "md5forencrypt"))
	message := apiPath + "-36cd479b6b5-" + string(text) + "-36cd479b6b5-" + hex.EncodeToString(digest[:])
	sealed := ecbEncrypt([]byte(message), []byte(eapiKey))
	return url.Values{"params": {strings.ToUpper(hex.EncodeToString(sealed))}}, nil
}

func randomKey() ([]byte, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	key := make([]byte, 16)
	for i := range key {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return nil, err
		}
		key[i] = alphabet[n.Int64()]
	}
	return key, nil
}

func pkcs7(plain []byte) []byte {
	n := aes.BlockSize - len(plain)%aes.BlockSize
	return append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(n)}, n)...)
}

// The keys below are fixed 16-byte constants or randomKey output, so
// aes.NewCipher cannot fail.

func cbcEncrypt(plain, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	out := pkcs7(plain)
	cipher.NewCBCEncrypter(block, []byte(weapiIV)).CryptBlocks(out, out)
	return out
}

func ecbEncrypt(plain, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	out := pkcs7(plain)
	for i := 0; i < len(out); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], out[i:i+aes.BlockSize])
	}
	return out
}
