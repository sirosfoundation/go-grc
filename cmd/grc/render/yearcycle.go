package render

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/yearcycle"
)

// Event is a type alias for rendering convenience.
type Event = yearcycle.Event

func generateYearCycle(cfg *config.Config, yc *yearcycle.YearCycle) error {
	dir := filepath.Join(cfg.SiteDir, "year-cycle")
	if err := writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Year Cycle", 8)); err != nil {
		return err
	}
	return writePage(filepath.Join(dir, "index.md"), renderYearCyclePage(yc))
}

func renderYearCyclePage(yc *yearcycle.YearCycle) string {
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Year Cycle\nsidebar_position: 1\ntitle: Year Cycle\n---\n\n")

	title := yc.Title
	if title == "" {
		title = "Year Cycle"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("Recurring governance, risk, and compliance activities projected onto a single calendar year.\n\n")

	if yc.CalendarURL != "" {
		fmt.Fprintf(&b, "📅 [Open calendar](%s)\n\n", yc.CalendarURL)
	}

	expanded := yc.ExpandAll()
	if len(expanded) == 0 {
		b.WriteString("_No recurring events found in the calendar._\n")
		return b.String()
	}

	// Render the SVG cycle diagram.
	b.WriteString(renderCycleSVG(expanded))
	b.WriteString("\n\n")

	// Month-by-month detail table.
	b.WriteString("## Monthly Schedule\n\n")
	currentMonth := time.Month(0)
	for _, ev := range expanded {
		if ev.Month != currentMonth {
			if currentMonth != 0 {
				b.WriteString("\n")
			}
			currentMonth = ev.Month
			fmt.Fprintf(&b, "### %s\n\n", currentMonth.String())
			b.WriteString("| Activity | Frequency | Key |\n")
			b.WriteString("|----------|-----------|-----|\n")
		}
		freq := ev.Frequency
		if freq == "" {
			freq = "one-time"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` |\n", ev.Title, freq, ev.Key)
	}
	b.WriteString("\n")

	return b.String()
}

// renderCycleSVG produces an inline SVG showing a circular year with events
// plotted around it. Each month is a 30° arc segment.
func renderCycleSVG(events []Event) string {
	const (
		size   = 500
		cx     = 250
		cy     = 250
		rOuter = 200
		rInner = 130
		rLabel = 220
		rDot   = 155
	)

	// Group events by month.
	byMonth := make(map[time.Month][]Event)
	for _, ev := range events {
		byMonth[ev.Month] = append(byMonth[ev.Month], ev)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" style="max-width:500px;margin:0 auto;display:block">`, size, size)
	b.WriteString("\n")

	// Background circle.
	fmt.Fprintf(&b, `  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#e2e8f0" stroke-width="2"/>`, cx, cy, rOuter)
	b.WriteString("\n")
	fmt.Fprintf(&b, `  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#e2e8f0" stroke-width="1"/>`, cx, cy, rInner)
	b.WriteString("\n")

	// Month segments and labels.
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	for i, name := range months {
		angle := float64(i)*30.0 - 90.0 // start at top (12 o'clock)
		midAngle := angle + 15.0
		radMid := midAngle * 3.14159265 / 180.0

		// Month label
		lx := float64(cx) + float64(rLabel)*cos(radMid)
		ly := float64(cy) + float64(rLabel)*sin(radMid)
		fmt.Fprintf(&b, `  <text x="%.1f" y="%.1f" text-anchor="middle" dominant-baseline="central" font-size="11" fill="#64748b">%s</text>`, lx, ly, name)
		b.WriteString("\n")

		// Segment separator line
		rad := angle * 3.14159265 / 180.0
		x1 := float64(cx) + float64(rInner)*cos(rad)
		y1 := float64(cy) + float64(rInner)*sin(rad)
		x2 := float64(cx) + float64(rOuter)*cos(rad)
		y2 := float64(cy) + float64(rOuter)*sin(rad)
		fmt.Fprintf(&b, `  <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#e2e8f0" stroke-width="1"/>`, x1, y1, x2, y2)
		b.WriteString("\n")

		// Event dots in this month.
		m := time.Month(i + 1)
		evts := byMonth[m]
		for j, ev := range evts {
			// Spread dots evenly within the month's 30° arc.
			dotAngle := angle + float64(j+1)*30.0/float64(len(evts)+1)
			dotRad := dotAngle * 3.14159265 / 180.0
			dx := float64(cx) + float64(rDot)*cos(dotRad)
			dy := float64(cy) + float64(rDot)*sin(dotRad)
			color := frequencyColor(ev.Frequency)
			fmt.Fprintf(&b, `  <circle cx="%.1f" cy="%.1f" r="5" fill="%s" stroke="white" stroke-width="1.5">`, dx, dy, color)
			fmt.Fprintf(&b, `<title>%s (%s)</title></circle>`, escapeXML(ev.Title), ev.Key)
			b.WriteString("\n")
		}
	}

	// Legend
	legendY := size - 20
	legendItems := []struct {
		color, label string
	}{
		{"#6366f1", "yearly"},
		{"#0ea5e9", "quarterly"},
		{"#22c55e", "monthly"},
		{"#94a3b8", "other"},
	}
	startX := 100
	for _, item := range legendItems {
		fmt.Fprintf(&b, `  <circle cx="%d" cy="%d" r="4" fill="%s"/>`, startX, legendY, item.color)
		fmt.Fprintf(&b, `  <text x="%d" y="%d" font-size="10" fill="#64748b" dominant-baseline="central">%s</text>`, startX+8, legendY, item.label)
		startX += 80
	}
	b.WriteString("\n")

	b.WriteString("</svg>\n")
	return b.String()
}

func frequencyColor(freq string) string {
	switch freq {
	case "yearly":
		return "#6366f1"
	case "quarterly":
		return "#0ea5e9"
	case "monthly":
		return "#22c55e"
	case "weekly":
		return "#22c55e"
	default:
		return "#94a3b8"
	}
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func cos(rad float64) float64 {
	return math.Cos(rad)
}

func sin(rad float64) float64 {
	return math.Sin(rad)
}
