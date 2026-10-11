package infographic

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestRenderEscapesUntrustedTextAndShowsFlow(t *testing.T) {
	plan := Plan{Title: "見出し <script>", Takeaway: "重要な結論 & 理由", Kind: "flow", Items: []Item{
		{Heading: "入力", Detail: "元記事を確認"},
		{Heading: "判断", Detail: "条件を比べる"},
		{Heading: "結果", Detail: "次の行動を選ぶ"},
	}}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	svg, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	var root struct{ XMLName xml.Name }
	if err := xml.Unmarshal(svg, &root); err != nil || root.XMLName.Local != "svg" {
		t.Fatalf("invalid SVG XML: %v", err)
	}
	if bytes.Contains(svg, []byte("<script>")) || !bytes.Contains(svg, []byte("&lt;script&gt;")) || !bytes.Contains(svg, []byte("重要な結論 &amp; 理由")) || !strings.Contains(string(svg), "次の行動を選ぶ") {
		t.Fatalf("unsafe or incomplete SVG: %s", svg)
	}
	plan.Kind = "cards"
	svg, err = Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(svg, &root); err != nil || !bytes.Contains(svg, []byte(`data-layout="cards"`)) {
		t.Fatalf("invalid card SVG: %v", err)
	}
}

func TestPlanRejectsExcessiveContent(t *testing.T) {
	plan := Plan{Title: "見出し", Takeaway: "結論", Kind: "cards", Items: []Item{{Heading: "一", Detail: "説明"}}}
	if plan.Validate() == nil {
		t.Fatal("accepted one item")
	}
	plan.Items = []Item{{Heading: "一", Detail: "説明"}, {Heading: "二", Detail: "説明"}, {Heading: "三", Detail: "説明"}}
	plan.Items[0].Detail = strings.Repeat("長", 101)
	if plan.Validate() == nil {
		t.Fatal("accepted overlong detail")
	}
}

func TestPlanAcceptsMeaningfulVisualStructures(t *testing.T) {
	base := Plan{Title: "記事の要点", Takeaway: "選ぶ条件で結果が変わる", Items: []Item{
		{Heading: "方法A", Detail: "初期費用が低い", Icon: "money"},
		{Heading: "方法B", Detail: "運用費用を抑える", Icon: "chart"},
	}}
	for _, kind := range []string{"comparison", "timeline", "cause", "hub", "cycle"} {
		plan := base
		plan.Kind = kind
		if kind != "comparison" {
			plan.Items = append(append([]Item(nil), base.Items...), Item{Heading: "結果", Detail: "選択の目安", Icon: "idea"})
		}
		if err := plan.Validate(); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		svg, err := Render(plan)
		if err != nil {
			t.Errorf("%s render: %v", kind, err)
			continue
		}
		if !bytes.Contains(svg, []byte(`data-layout="`+kind+`"`)) {
			t.Errorf("%s missing layout marker", kind)
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal(svg, &root); err != nil {
			t.Errorf("%s invalid XML: %v", kind, err)
		}
	}
	base.Kind = "comparison"
	base.Items = append(base.Items, Item{Heading: "余分", Detail: "比較対象が多い"})
	if base.Validate() == nil {
		t.Fatal("comparison accepted three sides")
	}
	base.Kind = "hub"
	base.Items[0].Icon = "<script>"
	if base.Validate() == nil {
		t.Fatal("accepted arbitrary icon")
	}
}
