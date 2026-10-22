// Package lyrics turns LRC and NetEase's word-timed YRC text into lines.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Word is one sung syllable with its own timing (milliseconds).
type Word struct {
	Start    int    `json:"t"`
	Duration int    `json:"d"`
	Text     string `json:"w"`
}

type Line struct {
	Start       int    `json:"t"` // milliseconds
	Text        string `json:"text"`
	Translation string `json:"tr,omitempty"`
	Words       []Word `json:"words,omitempty"`
}

type Lyrics struct {
	Lines   []Line   `json:"lines"`
	Credits []string `json:"credits,omitempty"` // 作词 / 作曲 … kept off the lyric lines
	Synced  bool     `json:"synced"`
}

var (
	lrcStamp = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	yrcLine  = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
	yrcWord  = regexp.MustCompile(`\((\d+),(\d+),\d+\)([^(]*)`)
	label    = regexp.MustCompile(`^\s*([^：:]{1,14}?)\s*[:：]\s*\S`)
)

// creditWords mark a "role: name" line as a credit rather than a lyric.
var creditWords = []string{"词", "曲", "编", "制作", "录音", "混音", "母带", "和声", "合声", "和音", "配唱", "吉他", "贝斯", "鼓",
	"键盘", "钢琴", "弦", "监制", "出品", "发行", "企划", "统筹", "策划", "演唱", "原唱", "后期", "人声", "打击", "笛", "琴", "工程", "助理",
	"op", "sp", "isrc", "lyric", "compos", "produc", "arrang", "mix", "master", "record", "vocal", "guitar", "bass", "drum", "keyboard", "string"}

func isCredit(text string) bool {
	m := label.FindStringSubmatch(text)
	if m == nil || strings.ContainsAny(m[1], "，。！？,.!?") {
		return false
	}
	role := strings.ToLower(m[1])
	for _, w := range creditWords {
		if strings.Contains(role, w) {
			return true
		}
	}
	return false
}

// Parse builds lyrics from LRC, an optional LRC translation and optional YRC.
// Word timings come from YRC when it is present.
func Parse(lrc, translation, yrc string) Lyrics {
	var out Lyrics
	lines := parseLRC(lrc, &out.Credits)
	if words := parseYRC(yrc, &out.Credits); len(words) > 0 {
		lines = words
	}
	if tr := parseLRC(translation, nil); len(tr) > 0 {
		byStart := map[int]string{}
		for _, l := range tr {
			byStart[l.Start] = l.Text
		}
		for i := range lines {
			lines[i].Translation = closest(byStart, lines[i].Start)
		}
	}
	out.Lines = lines
	out.Synced = len(lines) > 1 && lines[len(lines)-1].Start > 0
	if out.Lines == nil {
		out.Lines = []Line{}
	}
	return out
}

func parseLRC(text string, credits *[]string) []Line {
	var lines []Line
	for _, raw := range strings.Split(text, "\n") {
		stamps := lrcStamp.FindAllStringSubmatch(raw, -1)
		body := strings.TrimSpace(lrcStamp.ReplaceAllString(raw, ""))
		if len(stamps) == 0 || body == "" {
			continue
		}
		if isCredit(body) {
			if credits != nil {
				*credits = append(*credits, body)
			}
			continue
		}
		for _, s := range stamps {
			lines = append(lines, Line{Start: millis(s[1], s[2], s[3]), Text: body})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start })
	return lines
}

func parseYRC(text string, credits *[]string) []Line {
	var lines []Line
	for _, raw := range strings.Split(text, "\n") {
		m := yrcLine.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue // JSON credit blocks and blank lines
		}
		start, _ := strconv.Atoi(m[1])
		line := Line{Start: start}
		var b strings.Builder
		for _, w := range yrcWord.FindAllStringSubmatch(m[3], -1) {
			t, _ := strconv.Atoi(w[1])
			d, _ := strconv.Atoi(w[2])
			line.Words = append(line.Words, Word{Start: t, Duration: d, Text: w[3]})
			b.WriteString(w[3])
		}
		line.Text = strings.TrimSpace(b.String())
		if line.Text == "" {
			continue
		}
		if isCredit(line.Text) {
			*credits = append(*credits, line.Text)
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func millis(min, sec, frac string) int {
	m, _ := strconv.Atoi(min)
	s, _ := strconv.Atoi(sec)
	f := 0
	if frac != "" {
		f, _ = strconv.Atoi(frac)
		switch len(frac) {
		case 1:
			f *= 100
		case 2:
			f *= 10
		}
	}
	return (m*60+s)*1000 + f
}

// closest finds the translation stamped within half a second of start.
func closest(byStart map[int]string, start int) string {
	if text, ok := byStart[start]; ok {
		return text
	}
	best, gap := "", 501
	for t, text := range byStart {
		if d := abs(t - start); d < gap {
			best, gap = text, d
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
