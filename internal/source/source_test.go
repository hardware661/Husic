package source

import "testing"

func TestSameSong(t *testing.T) {
	studio := Track{Title: "大城小爱", Artists: []string{"王力宏"}, Duration: 224}
	cases := []struct {
		name string
		b    Track
		want bool
	}{
		{"same take elsewhere", Track{Title: "大城小爱", Artists: []string{"王力宏"}, Duration: 225}, true},
		{"subtitle in brackets", Track{Title: "大城小爱 (电影《XX》插曲)", Artists: []string{"王力宏", "某人"}, Duration: 222}, true},
		{"live cut", Track{Title: "大城小爱(Live)", Artists: []string{"王力宏"}, Duration: 222}, false},
		{"cover", Track{Title: "大城小爱", Artists: []string{"梓渝"}, Duration: 224}, false},
		{"DJ edit", Track{Title: "大城小爱 (DJ版)", Artists: []string{"王力宏"}, Duration: 224}, false},
		{"much longer", Track{Title: "大城小爱", Artists: []string{"王力宏"}, Duration: 260}, false},
		{"unknown length", Track{Title: "大城小爱", Artists: []string{"王力宏"}}, true},
	}
	for _, c := range cases {
		if got := SameSong(studio, c.b); got != c.want {
			t.Errorf("%s: SameSong = %v", c.name, got)
		}
	}
}
