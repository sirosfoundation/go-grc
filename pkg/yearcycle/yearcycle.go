// Package yearcycle loads recurring events from an iCal feed and
// projects them onto a single canonical year for cyclic visualization.
package yearcycle

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
)

// Event is a single recurring activity projected onto one canonical year.
type Event struct {
	// Key is a stable lookup identifier derived from the SUMMARY, e.g.
	// "dpia-review". Controls reference this key instead of raw calendar UIDs.
	Key string
	// Title is the human-readable event title (SUMMARY).
	Title string
	// Month is the 1-based month in which the event occurs (1 = January).
	Month time.Month
	// Day within the month.
	Day int
	// Frequency describes the recurrence: "monthly", "quarterly", "yearly", etc.
	Frequency string
	// CalendarURL links to the source calendar entry (if available).
	CalendarURL string
}

// YearCycle is the full set of recurring events for one calendar source.
type YearCycle struct {
	// Title for the calendar source.
	Title string
	// CalendarURL is the public URL of the iCal feed.
	CalendarURL string
	// Events projected onto a single year, sorted by month then day.
	Events []Event
}

// Load reads an iCal feed from a URL or local file path, extracts recurring
// events, and projects them onto a canonical year.
func Load(source, title string) (*YearCycle, error) {
	var r io.ReadCloser
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		resp, err := http.Get(source) //nolint:gosec // user-configured URL
		if err != nil {
			return nil, fmt.Errorf("fetching calendar: %w", err)
		}
		r = resp.Body
	} else {
		f, err := os.Open(source)
		if err != nil {
			return nil, fmt.Errorf("opening calendar file: %w", err)
		}
		r = f
	}
	defer func() { _ = r.Close() }()

	return Parse(r, title, source)
}

// Parse reads an iCal stream and returns a YearCycle.
func Parse(r io.Reader, title, calendarURL string) (*YearCycle, error) {
	cal, err := ics.ParseCalendar(r)
	if err != nil {
		return nil, fmt.Errorf("parsing iCal: %w", err)
	}

	yc := &YearCycle{
		Title:       title,
		CalendarURL: calendarURL,
	}

	for _, comp := range cal.Events() {
		ev := extractEvent(comp, calendarURL)
		if ev != nil {
			yc.Events = append(yc.Events, *ev)
		}
	}

	// Deduplicate by key+month (recurring events may expand to multiple
	// occurrences in the same month).
	yc.Events = dedup(yc.Events)

	sort.Slice(yc.Events, func(i, j int) bool {
		if yc.Events[i].Month != yc.Events[j].Month {
			return yc.Events[i].Month < yc.Events[j].Month
		}
		return yc.Events[i].Day < yc.Events[j].Day
	})

	return yc, nil
}

// KeyFromTitle derives a stable lookup key from a title string.
// "DPIA Review" → "dpia-review"
func KeyFromTitle(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, s)
	// collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// LookupByKey returns the first event matching the given key, or nil.
func (yc *YearCycle) LookupByKey(key string) *Event {
	for i := range yc.Events {
		if yc.Events[i].Key == key {
			return &yc.Events[i]
		}
	}
	return nil
}

func extractEvent(comp *ics.VEvent, calendarURL string) *Event {
	summary := propText(comp, ics.ComponentPropertySummary)
	if summary == "" {
		return nil
	}

	dtstart := propText(comp, ics.ComponentPropertyDtStart)
	if dtstart == "" {
		return nil
	}

	t, err := parseICalDate(dtstart)
	if err != nil {
		return nil
	}

	freq := ""
	if rrule := propText(comp, ics.ComponentPropertyRrule); rrule != "" {
		freq = parseFrequency(rrule)
	}

	return &Event{
		Key:         KeyFromTitle(summary),
		Title:       summary,
		Month:       t.Month(),
		Day:         t.Day(),
		Frequency:   freq,
		CalendarURL: calendarURL,
	}
}

func propText(comp *ics.VEvent, prop ics.ComponentProperty) string {
	p := comp.GetProperty(prop)
	if p == nil {
		return ""
	}
	return p.Value
}

func parseICalDate(s string) (time.Time, error) {
	// Try common iCal date-time formats.
	for _, layout := range []string{
		"20060102T150405Z",
		"20060102T150405",
		"20060102",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable date: %s", s)
}

func parseFrequency(rrule string) string {
	for _, part := range strings.Split(rrule, ";") {
		if strings.HasPrefix(part, "FREQ=") {
			switch strings.TrimPrefix(part, "FREQ=") {
			case "DAILY":
				return "daily"
			case "WEEKLY":
				return "weekly"
			case "MONTHLY":
				return "monthly"
			case "YEARLY":
				return "yearly"
			default:
				return strings.ToLower(strings.TrimPrefix(part, "FREQ="))
			}
		}
	}
	return ""
}

// expandRecurring generates all monthly occurrences for a recurring event
// within a single year.
func expandRecurring(ev Event) []Event {
	switch ev.Frequency {
	case "monthly":
		var out []Event
		for m := time.January; m <= time.December; m++ {
			e := ev
			e.Month = m
			out = append(out, e)
		}
		return out
	case "quarterly":
		// Start from the original month and step by 3.
		var out []Event
		start := int(ev.Month)
		for m := start; m <= 12; m += 3 {
			e := ev
			e.Month = time.Month(m)
			out = append(out, e)
		}
		return out
	case "weekly":
		// Weekly events occur in every month.
		var out []Event
		for m := time.January; m <= time.December; m++ {
			e := ev
			e.Month = m
			out = append(out, e)
		}
		return out
	default:
		return []Event{ev}
	}
}

// ExpandAll expands recurring events across the year.
func (yc *YearCycle) ExpandAll() []Event {
	var all []Event
	for _, ev := range yc.Events {
		all = append(all, expandRecurring(ev)...)
	}
	all = dedup(all)
	sort.Slice(all, func(i, j int) bool {
		if all[i].Month != all[j].Month {
			return all[i].Month < all[j].Month
		}
		return all[i].Day < all[j].Day
	})
	return all
}

func dedup(events []Event) []Event {
	seen := make(map[string]bool)
	var out []Event
	for _, ev := range events {
		k := fmt.Sprintf("%s:%d", ev.Key, ev.Month)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, ev)
	}
	return out
}
