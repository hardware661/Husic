package lyrics

import "testing"

func TestParseKeepsCreditsOffLinesAndJoinsTranslation(t *testing.T) {
	lrc := "[00:00.00] 作词 : 某人\n[00:00.50] 作曲 : 某人\n[00:12.34]第一句\n[00:15.5]第二句\n[01:02.003][01:30.000]副歌\n"
	tr := "[00:12.30]first line\n[00:15.50]second line\n"
	got := Parse(lrc, tr, "")
	if len(got.Credits) != 2 || len(got.Lines) != 4 || !got.Synced {
		t.Fatalf("credits=%v lines=%+v", got.Credits, got.Lines)
	}
	if got.Lines[0].Start != 12340 || got.Lines[0].Translation != "first line" {
		t.Fatalf("first line = %+v", got.Lines[0])
	}
	if got.Lines[1].Start != 15500 || got.Lines[2].Start != 62003 || got.Lines[3].Start != 90000 {
		t.Fatalf("stamps = %+v", got.Lines)
	}
}

func TestParsePrefersWordTimings(t *testing.T) {
	yrc := `{"t":0,"c":[{"tx":"作词: "},{"tx":"某人"}]}
[12340,2100](12340,600,0)第(12940,500,0)一(13440,1000,0)句
[15500,900](15500,900,0)第二句`
	got := Parse("[00:12.34]第一句\n[00:15.50]第二句", "", yrc)
	if len(got.Lines) != 2 || len(got.Lines[0].Words) != 3 || got.Lines[0].Words[1] != (Word{12940, 500, "一"}) {
		t.Fatalf("lines = %+v", got.Lines)
	}
	if got.Lines[0].Text != "第一句" {
		t.Fatalf("text = %q", got.Lines[0].Text)
	}
}

func TestCreditDetection(t *testing.T) {
	for _, line := range []string{"合声：周杰伦", "录音工程：杨瑞代（Alfa Studio）", "Producer: Someone", "OP : 某公司", "混音 / 母带：某人"} {
		if !isCredit(line) {
			t.Errorf("%q should be a credit", line)
		}
	}
	for _, line := range []string{"我说：别走", "时间：带走了一切，我们还在", "故事的小黄花"} {
		if isCredit(line) {
			t.Errorf("%q is a lyric", line)
		}
	}
}
