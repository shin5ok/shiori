package infographic

import (
	"bytes"
	"fmt"
)

// Render composes validated article facts into a layout selected for their relationship.
// Gemini supplies words and a small set of choices; it never supplies markup or coordinates.
func Render(p Plan) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	height := 960
	switch p.Kind {
	case "flow":
		height = 330 + len(p.Items)*185 + 70
	case "timeline":
		height = 350 + len(p.Items)*155 + 80
	case "cause":
		height = 520 + (len(p.Items)-2)*170
	case "comparison":
		height = 770
	case "hub", "cycle":
		height = 1090
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 %d" data-layout="%s" role="img" aria-labelledby="title desc"><title id="title">%s</title><desc id="desc">%s</desc><rect width="1200" height="%d" fill="#f7f9fc"/><rect width="1200" height="12" fill="#2459a8"/>`, height, p.Kind, escaped(p.Title), escaped(p.Takeaway), height)
	writeText(&b, 70, 78, 19, 28, 50, "#2459a8", "700", "SHIORI / VISUAL SUMMARY")
	writeText(&b, 70, 142, 42, 52, 24, "#17314e", "700", p.Title)
	if p.Kind != "hub" && p.Kind != "cycle" {
		b.WriteString(`<rect x="70" y="233" width="1060" height="82" rx="18" fill="#e7f0fc"/>`)
		writeText(&b, 99, 271, 27, 32, 35, "#1b4a83", "700", p.Takeaway)
	}
	switch p.Kind {
	case "flow":
		renderFlow(&b, p)
	case "cards":
		renderCards(&b, p)
	case "comparison":
		renderComparison(&b, p)
	case "timeline":
		renderTimeline(&b, p)
	case "cause":
		renderCause(&b, p)
	case "hub":
		renderHub(&b, p)
	case "cycle":
		renderCycle(&b, p)
	}
	writeText(&b, 70, height-28, 17, 22, 65, "#71839a", "400", "AIが要約から作成した図解です。正確な情報は元記事をご確認ください。")
	b.WriteString(`</svg>`)
	return b.Bytes(), nil
}

func writeText(b *bytes.Buffer, x, y, size, step, width int, color, weight, value string) {
	for i, line := range lines(value, width) {
		fmt.Fprintf(b, `<text x="%d" y="%d" fill="%s" font-family="sans-serif" font-size="%d" font-weight="%s">%s</text>`, x, y+i*step, color, size, weight, escaped(line))
	}
}

func icon(b *bytes.Buffer, name string, x, y int, accent string) {
	if name == "none" || name == "" {
		return
	}
	shape := ""
	switch name {
	case "document":
		shape = `<path d="M14 6h22l10 10v32H14z M36 6v10h10 M21 27h18 M21 35h18 M21 43h13"/>`
	case "cloud":
		shape = `<path d="M15 42h31a10 10 0 0 0 0-20 17 17 0 0 0-32 2 9 9 0 0 0 1 18z"/>`
	case "database":
		shape = `<ellipse cx="30" cy="13" rx="18" ry="7"/><path d="M12 13v31c0 9 36 9 36 0V13 M12 28c0 9 36 9 36 0"/>`
	case "clock":
		shape = `<circle cx="30" cy="30" r="21"/><path d="M30 16v15l11 7"/>`
	case "people":
		shape = `<circle cx="22" cy="20" r="7"/><circle cx="40" cy="22" r="6"/><path d="M8 47c1-11 6-17 14-17s14 6 14 17 M35 35c8-2 15 3 17 12"/>`
	case "chart":
		shape = `<path d="M10 48h43 M15 42V29h9v13 M29 42V19h9v23 M43 42V11h9v31"/>`
	case "shield":
		shape = `<path d="M30 7 48 14v16c0 12-8 20-18 24-10-4-18-12-18-24V14z M22 30l6 6 12-14"/>`
	case "idea":
		shape = `<path d="M22 42h16 M24 48h12 M30 7a18 18 0 0 0-10 33h20A18 18 0 0 0 30 7z M30 17v12 M24 23h12"/>`
	case "money":
		shape = `<circle cx="30" cy="30" r="22"/><path d="M38 19c-4-4-16-4-16 3 0 9 17 3 17 13 0 8-12 10-19 5 M30 13v34"/>`
	case "gear":
		shape = `<circle cx="30" cy="30" r="11"/><path d="M30 5v12 M30 43v12 M5 30h12 M43 30h12 M12 12l9 9 M39 39l9 9 M48 12l-9 9 M21 39l-9 9"/>`
	}
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="39" fill="#edf3fc"/><g transform="translate(%d %d) scale(.9)" fill="none" stroke="%s" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">%s</g>`, x+30, y+30, x, y, accent, shape)
}

func panel(b *bytes.Buffer, x, y, w, h int, fill, accent string, item Item, compact bool) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" rx="19" fill="%s" stroke="#d4e1ef" stroke-width="2"/><rect x="%d" y="%d" width="7" height="%d" rx="3" fill="%s"/>`, x, y, w, h, fill, x, y, h, accent)
	if compact {
		icon(b, item.Icon, x+20, y+21, accent)
		headingY, detailY, detailSize, detailStep := y+62, y+102, 21, 28
		if h < 145 {
			headingY, detailY, detailSize, detailStep = y+55, y+91, 19, 24
		} else if h < 185 {
			headingY, detailY, detailSize, detailStep = y+46, y+96, 17, 22
			writeText(b, x+105, headingY, 22, 26, (w-130)/22, "#17314e", "700", item.Heading)
			writeText(b, x+105, detailY, detailSize, detailStep, (w-130)/detailSize, "#42566e", "400", item.Detail)
			return
		}
		writeText(b, x+105, headingY, 26, 34, (w-130)/26, "#17314e", "700", item.Heading)
		writeText(b, x+105, detailY, detailSize, detailStep, (w-130)/detailSize, "#42566e", "400", item.Detail)
		return
	}
	icon(b, item.Icon, x+25, y+24, accent)
	writeText(b, x+30, y+140, 29, 37, (w-60)/29, "#17314e", "700", item.Heading)
	writeText(b, x+30, y+205, 23, 31, (w-60)/23, "#42566e", "400", item.Detail)
}

func satellitePanel(b *bytes.Buffer, x, y, w int, accent string, item Item) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="220" rx="19" fill="#fff" stroke="#d4e1ef" stroke-width="2"/>`, x, y, w)
	icon(b, item.Icon, x+17, y+12, accent)
	writeText(b, x+98, y+49, 20, 24, (w-116)/20, "#17314e", "700", item.Heading)
	writeText(b, x+23, y+137, 18, 23, (w-46)/18, "#42566e", "400", item.Detail)
}

func renderFlow(b *bytes.Buffer, p Plan) {
	for i, item := range p.Items {
		y := 340 + i*185
		if i < len(p.Items)-1 {
			fmt.Fprintf(b, `<path d="M600 %d v34 m-10 -10 10 10 10 -10" fill="none" stroke="#2459a8" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>`, y+149)
		}
		panel(b, 70, y, 1060, 148, "#ffffff", "#2459a8", item, true)
		fmt.Fprintf(b, `<circle cx="1080" cy="%d" r="20" fill="#2459a8"/><text x="1080" y="%d" text-anchor="middle" fill="#fff" font-family="sans-serif" font-size="18" font-weight="700">%d</text>`, y+74, y+81, i+1)
	}
}

func renderCards(b *bytes.Buffer, p Plan) {
	first := p.Items[0]
	panel(b, 70, 340, 520, 540, "#ffffff", "#2459a8", first, false)
	remaining := len(p.Items) - 1
	h := (540 - (remaining-1)*16) / remaining
	for i, item := range p.Items[1:] {
		y := 340 + i*(h+16)
		accent := []string{"#146b67", "#a45932", "#6d5ca8"}[i]
		panel(b, 610, y, 520, h, "#ffffff", accent, item, true)
	}
}

func renderComparison(b *bytes.Buffer, p Plan) {
	colors := []string{"#2459a8", "#a45932"}
	for i, item := range p.Items {
		x := 70 + i*540
		panel(b, x, 350, 520, 330, "#ffffff", colors[i], item, false)
		if item.Marker != "" {
			writeText(b, x+30, 650, 20, 24, 22, colors[i], "700", item.Marker)
		}
	}
	b.WriteString(`<circle cx="600" cy="515" r="36" fill="#17314e"/><text x="600" y="523" text-anchor="middle" fill="#fff" font-family="sans-serif" font-size="23" font-weight="700">VS</text>`)
}

func renderTimeline(b *bytes.Buffer, p Plan) {
	last := 350 + (len(p.Items)-1)*155
	fmt.Fprintf(b, `<path d="M182 362 V%d" fill="none" stroke="#83b7bd" stroke-width="6" stroke-linecap="round"/>`, last+48)
	for i, item := range p.Items {
		y := 350 + i*155
		fmt.Fprintf(b, `<circle cx="182" cy="%d" r="17" fill="#146b67" stroke="#fff" stroke-width="5"/>`, y+48)
		if item.Marker != "" {
			writeText(b, 70, y+55, 18, 22, 10, "#146b67", "700", item.Marker)
		}
		panel(b, 245, y, 885, 132, "#ffffff", "#146b67", item, true)
	}
}

func renderCause(b *bytes.Buffer, p Plan) {
	factors := p.Items[:len(p.Items)-1]
	result := p.Items[len(p.Items)-1]
	centerY := 340 + (len(factors)-1)*85 + 48
	for i, item := range factors {
		y := 340 + i*170
		fmt.Fprintf(b, `<path d="M520 %d C595 %d 595 %d 650 %d" fill="none" stroke="#917ac0" stroke-width="3"/><circle cx="650" cy="%d" r="5" fill="#917ac0"/>`, y+62, y+62, centerY, centerY, centerY)
		panel(b, 70, y, 450, 125, "#ffffff", "#6d5ca8", item, true)
	}
	resultY := centerY - 145
	panel(b, 650, resultY, 480, 290, "#eee9f7", "#6d5ca8", result, false)
	writeText(b, 670, resultY+268, 19, 24, 23, "#6d5ca8", "700", "影響・結果")
}

func renderHub(b *bytes.Buffer, p Plan) {
	positions := [][2]int{{70, 330}, {820, 330}, {70, 735}, {820, 735}}
	if len(p.Items) == 3 {
		positions[2] = [2]int{445, 765}
	}
	for i, item := range p.Items {
		x, y := positions[i][0], positions[i][1]
		cx, cy := x+155, y+110
		b.WriteString(fmt.Sprintf(`<path d="M%d %d L600 620" fill="none" stroke="#95b2d7" stroke-width="4"/>`, cx, cy))
		satellitePanel(b, x, y, 310, "#2459a8", item)
	}
	b.WriteString(`<rect x="408" y="512" width="384" height="212" rx="80" fill="#2459a8"/>`)
	writeText(b, 448, 572, 18, 24, 23, "#dceaff", "700", "中心となるポイント")
	writeText(b, 448, 623, 25, 32, 9, "#ffffff", "700", p.Takeaway)
}

func renderCycle(b *bytes.Buffer, p Plan) {
	positions := [][2]int{{70, 330}, {760, 330}, {760, 735}, {70, 735}}
	if len(p.Items) == 3 {
		positions[2] = [2]int{415, 735}
	}
	for i, item := range p.Items {
		x, y := positions[i][0], positions[i][1]
		satellitePanel(b, x, y, 370, "#146b67", item)
	}
	if len(p.Items) == 3 {
		b.WriteString(`<path d="M450 440 H748 m-13 -10 13 10 -13 10 M945 560 Q955 755 795 845 m14 -13 -14 13 16 7 M405 845 Q235 765 255 560 m-10 13 10 -13 10 13" fill="none" stroke="#146b67" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>`)
	} else {
		b.WriteString(`<path d="M450 440 H748 m-13 -10 13 10 -13 10 M945 560 V720 m-10 -13 10 13 10 -13 M750 845 H450 m13 -10 -13 10 13 10 M255 720 V560 m-10 13 10 -13 10 13" fill="none" stroke="#146b67" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>`)
	}
	b.WriteString(`<circle cx="600" cy="638" r="127" fill="#dcefeb"/>`)
	writeText(b, 504, 609, 18, 24, 12, "#146b67", "700", "繰り返すしくみ")
	writeText(b, 504, 653, 25, 31, 10, "#174b48", "700", p.Takeaway)
}
