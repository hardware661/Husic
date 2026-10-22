package source

import (
	"context"
	"strconv"
	"strings"

	"music/internal/netease"
)

// NetEase plays through the owner's account; it must reach NetEase from a
// mainland exit or most of the catalogue is region-blocked.
type NetEase struct {
	Client *netease.Client
	Level  string // standard, higher, exhigh, lossless
}

func (NetEase) Name() string { return "netease" }

// FromNetEase converts a NetEase song into a Track.
func FromNetEase(s netease.Song) Track {
	artists := make([]string, 0, len(s.Artists))
	for _, a := range s.Artists {
		artists = append(artists, a.Name)
	}
	return Track{Source: "netease", ID: strconv.FormatInt(s.ID, 10), Title: s.Name, Artists: artists,
		Album: s.Album.Name, Duration: int(s.Duration / 1000), Cover: s.Album.PicURL}
}

func (p NetEase) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	songs, privileges, err := p.Client.Search(ctx, query, limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for i, s := range songs {
		if i < len(privileges) && privileges[i].St < 0 {
			continue // removed from NetEase; another platform may still have it
		}
		out = append(out, FromNetEase(s))
	}
	return out, nil
}

func (p NetEase) Stream(ctx context.Context, t Track) (Stream, error) {
	id, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil {
		return Stream{}, ErrUnavailable
	}
	level := p.Level
	if level == "" {
		level = "exhigh"
	}
	urls, err := p.Client.SongURLs(ctx, []int64{id}, level)
	if err != nil {
		return Stream{}, err
	}
	if len(urls) == 0 || urls[0].URL == "" || urls[0].Trial() {
		return Stream{}, ErrUnavailable
	}
	u := urls[0]
	// The CDN serves the same path over TLS; plain http would be blocked as
	// mixed content inside the HTTPS app.
	return Stream{URL: strings.Replace(u.URL, "http://", "https://", 1), Bitrate: u.Br, Format: u.Type, Seconds: int(u.Time / 1000)}, nil
}
