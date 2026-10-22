package source

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recommendationTransport func(*http.Request) (*http.Response, error)

func (f recommendationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMiguPublicRecommendationsCarryPlayableIdentifiers(t *testing.T) {
	h := &HTTP{client: &http.Client{Transport: recommendationTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "app.c.nf.migu.cn" || r.URL.Query().Get("scene") != "TODAY_RECOMMEND" {
			t.Fatal("not the official recommendation endpoint")
		}
		body := `{"code":"000000","data":{"songItemList":[{"contentId":"content","songName":"Song","singerList":[{"name":"Artist"}],"duration":221,"img3":"/cover.webp","copyrightId":"copyright","songId":"song","albumId":"album"}]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	tracks, err := (Migu{HTTP: h}).Recommend(context.Background(), 20)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("recommendations: %v %v", tracks, err)
	}
	got := tracks[0]
	if got.ID != "content" || got.Extra["copyrightId"] != "copyright" || got.Cover != "https://d.musicapp.migu.cn/cover.webp" || got.Duration != 221 {
		t.Fatalf("incomplete playable metadata: %+v", got)
	}
}
