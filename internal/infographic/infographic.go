package infographic

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"unicode/utf8"
)

// Plan is the small, validated description of one visual summary.
type Plan struct {
	Title    string `json:"title" firestore:"title"`
	Takeaway string `json:"takeaway" firestore:"takeaway"`
	Kind     string `json:"kind" firestore:"kind"`
	Items    []Item `json:"items" firestore:"items"`
}

type Item struct {
	Heading string `json:"heading" firestore:"heading"`
	Detail  string `json:"detail" firestore:"detail"`
	Icon    string `json:"icon,omitempty" firestore:"icon"`
	Marker  string `json:"marker,omitempty" firestore:"marker"`
}

func (p Plan) Validate() error {
	limits := map[string][2]int{"flow": {3, 5}, "cards": {3, 4}, "comparison": {2, 2}, "timeline": {3, 5}, "cause": {3, 5}, "hub": {3, 4}, "cycle": {3, 4}}
	limit, known := limits[p.Kind]
	if !known {
		return errors.New("invalid infographic layout")
	}
	if !validText(p.Title, 40) || !validText(p.Takeaway, 60) || len(p.Items) < limit[0] || len(p.Items) > limit[1] {
		return errors.New("invalid infographic structure")
	}
	for _, item := range p.Items {
		if !validText(item.Heading, 22) || !validText(item.Detail, 56) || item.Marker != "" && !validText(item.Marker, 14) || !validIcon(item.Icon) {
			return errors.New("invalid infographic item")
		}
	}
	if p.Kind == "cause" {
		for _, item := range p.Items[:len(p.Items)-1] {
			if utf8.RuneCountInString(item.Heading) > 12 || utf8.RuneCountInString(item.Detail) > 30 {
				return errors.New("cause label is too long")
			}
		}
	}
	if (p.Kind == "hub" || p.Kind == "cycle") && utf8.RuneCountInString(p.Takeaway) > 30 {
		return errors.New("central message is too long")
	}
	return nil
}

func validIcon(icon string) bool {
	switch icon {
	case "", "none", "document", "cloud", "database", "clock", "people", "chart", "shield", "idea", "money", "gear":
		return true
	default:
		return false
	}
}

func validText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func escaped(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func lines(value string, width int) []string {
	runes := []rune(strings.TrimSpace(value))
	var out []string
	for len(runes) > 0 {
		end := min(width, len(runes))
		out = append(out, string(runes[:end]))
		runes = runes[end:]
	}
	return out
}
