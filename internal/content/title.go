package content

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxTitleRunes is the longest title shown as is; longer ones are shortened.
const MaxTitleRunes = 60

var titleSeparators = regexp.MustCompile(`\s+[|\-–—:/]+\s+|\s*｜\s*|\s*\|\s*`)

// ShortenTitle keeps a short title untouched. For a long one it drops site
// names and other suffixes by taking the longest segment between separators,
// then cuts a still-too-long segment at its last natural break.
func ShortenTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) <= MaxTitleRunes {
		return title
	}
	best := ""
	for _, part := range titleSeparators.Split(title, -1) {
		part = strings.TrimSpace(part)
		if utf8.RuneCountInString(part) > utf8.RuneCountInString(best) {
			best = part
		}
	}
	runes := []rune(best)
	if len(runes) <= MaxTitleRunes {
		return best
	}
	// Prefer a phrase boundary in the second half of the allowed length.
	cut := -1
	for i := MaxTitleRunes - 1; i >= MaxTitleRunes/2; i-- {
		if strings.ContainsRune("。！？!?、，,：: 　", runes[i]) {
			cut = i
			break
		}
	}
	if cut >= 0 {
		return strings.TrimRight(string(runes[:cut]), "。、，,：: 　") + "…"
	}
	return string(runes[:MaxTitleRunes-1]) + "…"
}
