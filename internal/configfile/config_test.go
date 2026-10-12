package configfile

import (
	"reflect"
	"testing"
)

func TestUnifiedConfig(t *testing.T) {
	c, err := Parse([]byte("accounts: [Reader@Example.com]\ndomains: [data-cloud.jp]\ntags: [登山, アウトドア, 旅行]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Tags, []string{"登山", "アウトドア", "旅行"}) || !c.Allowlist.Allows("reader@example.com") || !c.Allowlist.Allows("user@data-cloud.jp") || c.Allowlist.Allows("user@other.com") {
		t.Fatalf("config: %+v", c)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, input := range []string{
		"", "domains: [example.com]", "domains: [example.com]\ntags: [AI]",
		"domains: [example.com]\ntags: [AI, AI]", "domains: [example.com]\ntags: [AI, ' ']",
		"domains: [example.com]\ntags: [AI, 'a,b']", "tags: [AI, 技術]",
		"accounts: [invalid]\ntags: [AI, 技術]", "domains: [example.com]\ntags: [AI, 技術]\nunknown: true",
		"domains: [example.com]\ntags: [AI, 技術]\n---\ntags: [AI, 技術]",
		"domains: [example.com]\ntags: [AI, 技術]\ntags: [AI, OSS]",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted invalid config: %q", input)
		}
	}
}
