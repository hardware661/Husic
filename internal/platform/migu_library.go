package platform

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"music/internal/source"
)

func miguImage(v any) string {
	s := text(v)
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") {
		s = "https://d.musicapp.migu.cn" + s
	}
	return imageURL(s)
}

func miguCollectionInfo(row map[string]any) (Collection, bool) {
	id := first(row, "musicListId", "playlistId")
	if id == "" && text(row["resourceType"]) == "2021" {
		id = first(row, "resourceId", "id")
	}
	name := first(row, "title", "musicListName", "playlistName", "name")
	if !miguContentID.MatchString(id) || name == "" {
		return Collection{}, false
	}
	cover := first(row, "img", "img1", "cover", "coverUrl", "originalImgUrl")
	if cover == "" {
		cover = first(object(row["imgItem"]), "img", "webpImg", "imgOri")
	}
	count := integer(row["musicNum"])
	if count == 0 {
		count = integer(row["songCount"])
	}
	return Collection{ID: id, Name: name, Cover: miguImage(cover), Count: count}, true
}

// The home-page response groups liked songs and user playlists under separate
// components. Only explicit playlist-shaped resources are accepted; song IDs,
// user IDs and opaque navigation URLs never become collection identities.
func miguCollectCollections(v any, depth int, out *[]Collection, seen map[string]bool) bool {
	if depth > 6 || len(*out) >= 1000 {
		return false
	}
	if row := object(v); row != nil {
		if collection, ok := miguCollectionInfo(row); ok {
			if !seen[collection.ID] {
				seen[collection.ID] = true
				*out = append(*out, collection)
			}
			return true
		}
		recognized := false
		for _, k := range []string{"data", "content", "list", "items", "musicList", "musicLists", "musicListList", "playlistList", "playList", "myLike", "myLikeList", "myLikeMusicList", "likeMusicList", "likePlaylist", "myMusicList", "myCreateList", "myCollectList"} {
			if miguCollectCollections(row[k], depth+1, out, seen) {
				recognized = true
			}
		}
		return recognized
	}
	items := array(v)
	if items == nil {
		return false
	}
	recognized := len(items) == 0
	for _, item := range items {
		if miguCollectCollections(item, depth+1, out, seen) {
			recognized = true
		}
	}
	return recognized
}

func (c *Client) miguCollections(ctx context.Context) ([]Collection, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	if err := MiguValidateSession(s); err != nil {
		return nil, err
	}
	out, seen := []Collection{}, map[string]bool{}
	// The home page is also the canonical source of the user's liked-list ID.
	_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/pc/user/home-page/v2.0", nil, nil, nil, s.Cookies)
	if err != nil {
		return nil, err
	}
	d, err := miguBody(raw, "migu.library.home")
	if err != nil {
		return nil, err
	}
	if d["data"] == nil {
		return nil, ErrResponse
	}
	if !miguCollectCollections(d["data"], 0, &out, seen) {
		return nil, ErrResponse
	}
	for page := 1; page <= 10; page++ {
		_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/user/h5/my-music-list/v1.0", url.Values{"queryType": {"0"}, "pageNo": {strconv.Itoa(page)}, "pageSize": {"100"}}, nil, nil, s.Cookies)
		if err != nil {
			return nil, err
		}
		d, err := miguBody(raw, "migu.library.playlists")
		if err != nil {
			return nil, err
		}
		if d["data"] == nil {
			return nil, ErrResponse
		}
		pageRows := []Collection{}
		if !miguCollectCollections(d["data"], 0, &pageRows, map[string]bool{}) {
			return nil, ErrResponse
		}
		for _, collection := range pageRows {
			if !seen[collection.ID] {
				seen[collection.ID] = true
				out = append(out, collection)
			}
		}
		if len(pageRows) < 100 {
			break
		}
	}
	return out, nil
}

func miguTrack(row map[string]any) (source.Track, bool) {
	id, title := text(row["contentId"]), first(row, "songName", "name", "title")
	if !miguContentID.MatchString(id) || title == "" {
		return source.Track{}, false
	}
	t := source.Track{Source: "migu", ID: id, Title: title, Duration: integer(row["duration"]), Album: first(row, "album", "albumName"), Artists: []string{}, Extra: map[string]string{}}
	for _, key := range []string{"copyrightId", "songId", "albumId"} {
		if value := text(row[key]); miguAccountID.MatchString(value) {
			t.Extra[key] = value
		}
	}
	for _, v := range array(row["singerList"]) {
		if name := text(object(v)["name"]); name != "" {
			t.Artists = append(t.Artists, name)
		}
	}
	if len(t.Artists) == 0 {
		for _, v := range array(row["singers"]) {
			if name := text(object(v)["name"]); name != "" {
				t.Artists = append(t.Artists, name)
			}
		}
	}
	t.Cover = miguImage(first(row, "img3", "img2", "img1", "img", "cover"))
	if t.Cover == "" {
		t.Cover = miguImage(object(row["imgItem"])["img"])
	}
	return t, len(t.Artists) > 0
}

func (c *Client) miguCollection(ctx context.Context, id string, page int) (CollectionPage, error) {
	s := c.sessionCopy()
	result := CollectionPage{Tracks: []source.Track{}}
	if s.ID == "" {
		return result, ErrNotConnected
	}
	if err := MiguValidateSession(s); err != nil {
		return result, err
	}
	if !miguContentID.MatchString(id) || page < 1 || page > 100 {
		return result, ErrResponse
	}
	_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/resource/h5/my/playlist/song/v1.0", url.Values{"playlistId": {id}, "pageNo": {strconv.Itoa(page)}, "pageSize": {"50"}}, nil, nil, s.Cookies)
	if err != nil {
		return result, err
	}
	d, err := miguBody(raw, "migu.library.tracks")
	if err != nil {
		return result, err
	}
	data := object(d["data"])
	if data["songList"] == nil || data["totalCount"] == nil {
		return result, ErrResponse
	}
	if got := first(data, "playlistId", "musicListId"); got != "" && got != id {
		return result, ErrResponse
	}
	result.Total = integer(data["totalCount"])
	if result.Total < 0 || result.Total > 5000 {
		return result, ErrResponse
	}
	result.Collection = Collection{ID: id, Name: "咪咕歌单", Count: result.Total}
	if info, ok := miguCollectionInfo(data); ok {
		result.Collection = info
	}
	for _, v := range array(data["songList"]) {
		if track, ok := miguTrack(object(v)); ok {
			result.Tracks = append(result.Tracks, track)
		}
	}
	if page*50 < result.Total {
		if len(array(data["songList"])) == 0 {
			return result, ErrResponse
		}
		result.Next = page + 1
	}
	return result, nil
}

func (c *Client) miguRecommendations(ctx context.Context) ([]source.Track, error) {
	s := c.sessionCopy()
	if s.ID == "" {
		return nil, ErrNotConnected
	}
	if err := MiguValidateSession(s); err != nil {
		return nil, err
	}
	_, raw, err := c.miguRequest(ctx, "app.c.nf.migu.cn", "/pc/v1.0/template/todayRecommendList/release", url.Values{"actionId": {"1"}, "index": {"1"}, "templateVersion": {"5"}}, nil, nil, s.Cookies)
	if err != nil {
		return nil, err
	}
	d, err := miguBody(raw, "migu.recommendations")
	if err != nil {
		return nil, err
	}
	tracks := []source.Track{}
	seen := map[string]bool{}
	var walk func(any, int)
	walk = func(v any, depth int) {
		if depth > 6 || len(tracks) >= 100 {
			return
		}
		if row := object(v); row != nil {
			if track, ok := miguTrack(row); ok {
				if !seen[track.ID] {
					seen[track.ID] = true
					tracks = append(tracks, track)
				}
				return
			}
			for _, key := range []string{"data", "content", "contents", "items", "list", "songList", "songs", "dataList", "resourceList", "modules"} {
				walk(row[key], depth+1)
			}
			return
		}
		for _, item := range array(v) {
			walk(item, depth+1)
		}
	}
	walk(d["data"], 0)
	if len(tracks) == 0 {
		return nil, ErrResponse
	}
	return tracks, nil
}
