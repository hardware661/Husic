package platform

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"strings"
)

// Public encryption key embedded in the standard Kugou client protocol, not a
// private key or a user credential. Lite uses a different key and is not used.
const kugouPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDIAG7QOELSYoIJvTFJhMpe1s/gbjDJX51HBNnEl5HXqTW6lQ7LC8jr9fWZTwusknp+sVGzwd40MwP6U5yDE27M/X1+UR4tvOGOqp94TJtQ1EPnWGWXngpeIW5GxoQGao1rmYWAu6oi1z9XkChrsUdC6DJE5E221wf/4WLFxwAtRQIDAQAB
-----END PUBLIC KEY-----`

func kugouRSA(value any, pkcs1 bool) (string, error) {
	block, _ := pem.Decode([]byte(kugouPublicKey))
	if block == nil {
		return "", ErrResponse
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", ErrResponse
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", ErrResponse
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", ErrResponse
	}
	if pkcs1 {
		encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, key, body)
		if err != nil {
			return "", ErrResponse
		}
		return hex.EncodeToString(encrypted), nil
	}
	// The profile endpoint uses its legacy raw-RSA format: JSON is left
	// aligned, right padded with zero bytes to the modulus size (not PKCS1).
	if len(body) > key.Size() {
		return "", ErrResponse
	}
	padded := make([]byte, key.Size())
	copy(padded, body)
	n := new(big.Int).SetBytes(padded)
	if n.Cmp(key.N) >= 0 {
		return "", ErrResponse
	}
	encrypted := new(big.Int).Exp(n, big.NewInt(int64(key.E)), key.N).FillBytes(make([]byte, key.Size()))
	return strings.ToUpper(hex.EncodeToString(encrypted)), nil
}

func kugouAESKey(secret string) (cipher.Block, []byte, error) {
	digest := kugouMD5(secret)
	block, err := aes.NewCipher([]byte(digest[:16]))
	if err != nil {
		return nil, nil, ErrResponse
	}
	return block, []byte(digest[16:]), nil
}

func kugouAESEncrypt(value any, secret string) ([]byte, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return nil, ErrResponse
	}
	block, iv, err := kugouAESKey(secret)
	if err != nil {
		return nil, err
	}
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	for range padding {
		plain = append(plain, byte(padding))
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	return []byte(base64.StdEncoding.EncodeToString(encrypted)), nil
}

func kugouAESDecrypt(encrypted []byte, secret string) (map[string]any, error) {
	block, iv, err := kugouAESKey(secret)
	if err != nil || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, ErrResponse
	}
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, encrypted)
	padding := int(plain[len(plain)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(plain) {
		return nil, ErrResponse
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, ErrResponse
		}
	}
	return decode(plain[:len(plain)-padding])
}

func (c *Client) kugouRegisterDevice(ctx context.Context, s Session) (string, error) {
	if err := kugouValidate(s); err != nil {
		return "", err
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", ErrUnavailable
	}
	secret := hex.EncodeToString(random)[:6]
	// Synthetic application-owned device only; never inspect the owner's
	// hardware, phone, network, browser fingerprint or local machine.
	guid := kugouMD5(s.Cookies["KUGOU_API_MID"])
	device := map[string]any{
		"availableRamSize": 4983533568, "availableRomSize": 48114719, "availableSDSize": 48114717,
		"basebandVer": "", "batteryLevel": 100, "batteryStatus": 3, "brand": "Android", "buildSerial": "unknown", "device": "music", "imei": guid, "imsi": "", "manufacturer": "Android", "uuid": guid,
	}
	for _, name := range []string{"accelerometer", "gravity", "gyroscope", "light", "magnetic", "orientation", "pressure", "step_counter", "temperature"} {
		device[name], device[name+"Value"] = false, ""
	}
	body, err := kugouAESEncrypt(device, secret)
	if err != nil {
		return "", err
	}
	p, err := kugouRSA(map[string]any{"aes": secret, "uid": json.Number(s.ID), "token": s.Cookies["token"]}, true)
	if err != nil {
		return "", err
	}
	raw, err := c.kugouRequest(ctx, "userservice.kugou.com", "/risk/v2/r_register_dev", "", url.Values{"part": {"1"}, "platid": {"1"}, "p": {p}}, body, s.Cookies, false)
	if err != nil {
		return "", err
	}
	d, err := kugouAESDecrypt(raw, secret)
	if err != nil {
		return "", err
	}
	dfid := text(object(d["data"])["dfid"])
	if integer(d["status"]) != 1 || dfid == "-" || len(dfid) > 256 || !validCookie(dfid) {
		return "", ErrResponse
	}
	return dfid, nil
}
