package content

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
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

// IsJapanese reports whether a title reads as Japanese: it has kana, or kanji
// without any Latin letters.
func IsJapanese(title string) bool {
	han, latin := false, false
	for _, r := range title {
		switch {
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			return true
		case unicode.Is(unicode.Han, r):
			han = true
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			latin = true
		}
	}
	return han && !latin
}

// MarkGenerated wraps a title the AI wrote in double quotes, so readers can
// tell it is not the page's own title.
func MarkGenerated(title string) string {
	title = strings.TrimSpace(ShortenTitle(title))
	return `"` + title + `"`
}

// IsMarkedGenerated reports whether the title is already wrapped by MarkGenerated.
func IsMarkedGenerated(title string) bool {
	return utf8.RuneCountInString(title) > 2 && strings.HasPrefix(title, `"`) && strings.HasSuffix(title, `"`)
}

var embeddedURL = regexp.MustCompile(`(?i)https?://`)

// IsMeaninglessTitle reports titles that tell a reader nothing: empty, the
// site's own host name, or text that embeds a URL (for example a post that
// is only a t.co link).
func IsMeaninglessTitle(title, pageURL string) bool {
	title = strings.TrimSpace(title)
	if title == "" || embeddedURL.MatchString(title) {
		return true
	}
	if u, err := url.Parse(pageURL); err == nil && u.Hostname() != "" {
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		return strings.TrimPrefix(strings.ToLower(title), "www.") == host
	}
	return false
}
