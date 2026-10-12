package summary

import "testing"

func TestAutomaticTagVocabulary(t *testing.T) {
	for _, tc := range []struct {
		tags  []string
		valid bool
	}{
		{[]string{"AI", "技術"}, true}, {[]string{"酒", "グルメ", "旅行"}, true},
		{nil, false}, {[]string{"AI"}, false}, {[]string{"AI", "AI"}, false},
		{[]string{"AI", "未定義"}, false}, {[]string{"AI", "技術", "OSS", "SRE"}, false},
	} {
		if got := ValidAutoTags(tc.tags, []string{"AI", "技術", "酒", "グルメ", "旅行", "OSS", "SRE"}); got != tc.valid {
			t.Fatalf("%v: %v", tc.tags, got)
		}
	}
}
