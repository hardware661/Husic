package platform

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// The web client's signature uses public image keys, not an account secret.
func (c *Client) biliWBI(ctx context.Context, endpoint string, q url.Values, cookies map[string]string) (map[string]any, error) {
	var err error
	cookies, err = c.biliCookies(ctx, cookies)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	key, until := c.wbiKey, c.wbiUntil
	c.mu.RUnlock()
	if key == "" || time.Now().After(until) {
		_, raw, err := c.request(ctx, "api.bilibili.com", "/x/web-interface/nav", nil, nil, nil, cookies)
		if err != nil {
			return nil, err
		}
		d, err := decode(raw)
		if err != nil || d["code"] == nil || integer(d["code"]) != 0 && integer(d["code"]) != -101 {
			return nil, ErrResponse
		}
		images := object(object(d["data"])["wbi_img"])
		key, err = wbiKey(text(images["img_url"]), text(images["sub_url"]))
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.wbiKey, c.wbiUntil = key, time.Now().Add(6*time.Hour)
		c.mu.Unlock()
	}
	query := url.Values{}
	strip := strings.NewReplacer("!", "", "'", "", "(", "", ")", "", "*", "")
	for k, values := range q {
		for _, v := range values {
			query.Add(k, strip.Replace(v))
		}
	}
	query.Set("wts", strconv.FormatInt(time.Now().Unix(), 10))
	encoded := strings.ReplaceAll(query.Encode(), "+", "%20")
	sum := md5.Sum([]byte(encoded + key))
	query.Set("w_rid", hex.EncodeToString(sum[:]))
	d, _, err := c.biliCall(ctx, "api.bilibili.com", endpoint, query, cookies)
	return d, err
}
func wbiKey(image, sub string) (string, error) {
	get := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
	}
	input := get(image) + get(sub)
	if len(input) != 64 {
		return "", ErrResponse
	}
	order := []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13}
	result := make([]byte, 32)
	for i, n := range order {
		result[i] = input[n]
	}
	return string(result), nil
}
