package content

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortenTitle(t *testing.T) {
	long := strings.Repeat("あ", 80)
	for _, tc := range []struct{ name, in, want string }{
		{"short stays", "Goの並行処理入門", "Goの並行処理入門"},
		{"whitespace and newlines collapse", " Goの\n並行処理  入門 ", "Goの 並行処理 入門"},
		{"hyphenated words stay", "go-lang-notes", "go-lang-notes"},
		{"site name suffix dropped", "Cloud RunでIAPを使ってアプリケーションを安全に公開する手順と、つまずきやすい設定のまとめ - Example Tech Blog", "Cloud RunでIAPを使ってアプリケーションを安全に公開する手順と、つまずきやすい設定のまとめ"},
		{"fullwidth bar", "短い見出し｜" + strings.Repeat("長い本題", 20) + "を解説", strings.Repeat("長い本題", 15)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ShortenTitle(tc.in)
			if utf8.RuneCountInString(tc.in) > MaxTitleRunes && utf8.RuneCountInString(got) > MaxTitleRunes {
				t.Fatalf("too long: %q", got)
			}
			if tc.name != "fullwidth bar" && got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	if got := ShortenTitle(long); utf8.RuneCountInString(got) != MaxTitleRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("hard cut: %q", got)
	}
	if got := ShortenTitle(strings.Repeat("あ", 30) + "、" + strings.Repeat("い", 50)); got != strings.Repeat("あ", 30)+"…" {
		t.Fatalf("phrase cut: %q", got)
	}
}
