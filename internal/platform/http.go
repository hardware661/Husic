package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// All callers supply compiled platform origins and paths, never a URL from
// the browser, a QR callback, a playlist item or an upstream redirect.
func (c *Client) request(ctx context.Context, host, path string, query url.Values, form url.Values, body any, cookies map[string]string) (*http.Response, []byte, error) {
	var reader io.Reader
	method := http.MethodGet
	if form != nil {
		method = http.MethodPost
		reader = strings.NewReader(form.Encode())
	} else if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, ErrResponse
		}
		method = http.MethodPost
		reader = bytes.NewReader(encoded)
	}
	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	r, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Safari/537.36")
	r.Header.Set("Accept", "application/json, text/plain, */*")
	r.Header.Set("Referer", "https://www.bilibili.com/")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cookies {
		if !validCookie(v) {
			return nil, nil, ErrExpired
		}
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	response, err := c.direct.Do(r)
	if err != nil {
		return nil, nil, failure(host+path, -1, ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode == 401 {
		return nil, nil, ErrExpired
	}
	if response.StatusCode != 200 && response.StatusCode != 302 && response.StatusCode != 303 {
		return nil, nil, failure(host+path, response.StatusCode, fmt.Errorf("平台暂时无法连接（%d）", response.StatusCode))
	}
	const limit = int64(4 << 20)
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, nil, ErrResponse
	}
	return response, raw, nil
}
func object(v any) map[string]any { x, _ := v.(map[string]any); return x }
func array(v any) []any           { x, _ := v.([]any); return x }
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func integer(v any) int { n, _ := strconv.Atoi(text(v)); return n }
func decode(raw []byte) (map[string]any, error) {
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil || v == nil {
		return nil, ErrResponse
	}
	return v, nil
}
func imageURL(v any) string {
	s := text(v)
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return s
}
