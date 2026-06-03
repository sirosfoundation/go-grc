package yearcycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//Test//EN
BEGIN:VEVENT
DTSTART:20260115T090000Z
SUMMARY:DPIA Review
RRULE:FREQ=YEARLY
END:VEVENT
BEGIN:VEVENT
DTSTART:20260301T100000Z
SUMMARY:Risk Register Review
RRULE:FREQ=QUARTERLY
END:VEVENT
BEGIN:VEVENT
DTSTART:20260110T080000Z
SUMMARY:Security Standup
RRULE:FREQ=MONTHLY
END:VEVENT
BEGIN:VEVENT
DTSTART:20260620T140000Z
SUMMARY:Annual Penetration Test
END:VEVENT
END:VCALENDAR`

func TestParse(t *testing.T) {
	yc, err := Parse(strings.NewReader(testICS), "Test Cycle", "https://example.com/cal.ics")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if yc.Title != "Test Cycle" {
		t.Errorf("Title = %q, want %q", yc.Title, "Test Cycle")
	}
	if len(yc.Events) != 4 {
		t.Fatalf("got %d events, want 4", len(yc.Events))
	}
}

func TestKeyFromTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"DPIA Review", "dpia-review"},
		{"Risk Register Review", "risk-register-review"},
		{"  Annual Penetration Test  ", "annual-penetration-test"},
		{"ISO 27001 Audit", "iso-27001-audit"},
	}
	for _, tc := range tests {
		got := KeyFromTitle(tc.in)
		if got != tc.want {
			t.Errorf("KeyFromTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLookupByKey(t *testing.T) {
	yc, err := Parse(strings.NewReader(testICS), "Test", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ev := yc.LookupByKey("dpia-review")
	if ev == nil {
		t.Fatal("LookupByKey returned nil for dpia-review")
	}
	if ev.Month != time.January {
		t.Errorf("Month = %v, want January", ev.Month)
	}
	if ev.Frequency != "yearly" {
		t.Errorf("Frequency = %q, want yearly", ev.Frequency)
	}
}

func TestLookupByKeyMissing(t *testing.T) {
	yc, _ := Parse(strings.NewReader(testICS), "Test", "")
	if ev := yc.LookupByKey("nonexistent"); ev != nil {
		t.Error("expected nil for missing key")
	}
}

func TestExpandAll(t *testing.T) {
	yc, err := Parse(strings.NewReader(testICS), "Test", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expanded := yc.ExpandAll()
	// Monthly event: 12 months, quarterly: 4 (Mar, Jun, Sep, Dec), yearly: 1, one-time: 1 = 18
	if len(expanded) < 15 {
		t.Errorf("ExpandAll returned %d events, expected at least 15", len(expanded))
	}
	// Check sorted by month
	for i := 1; i < len(expanded); i++ {
		if expanded[i].Month < expanded[i-1].Month {
			t.Errorf("events not sorted: month %v before %v", expanded[i].Month, expanded[i-1].Month)
		}
	}
}

func TestExpandQuarterly(t *testing.T) {
	ev := Event{
		Key:       "test",
		Title:     "Test",
		Month:     time.March,
		Day:       1,
		Frequency: "quarterly",
	}
	expanded := expandRecurring(ev)
	if len(expanded) != 4 {
		t.Fatalf("quarterly expansion: got %d, want 4", len(expanded))
	}
	wantMonths := []time.Month{time.March, time.June, time.September, time.December}
	for i, want := range wantMonths {
		if expanded[i].Month != want {
			t.Errorf("expanded[%d].Month = %v, want %v", i, expanded[i].Month, want)
		}
	}
}

func TestParseFrequency(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"FREQ=YEARLY", "yearly"},
		{"FREQ=MONTHLY;INTERVAL=1", "monthly"},
		{"FREQ=WEEKLY;BYDAY=MO", "weekly"},
		{"FREQ=DAILY", "daily"},
		{"", ""},
	}
	for _, tc := range tests {
		got := parseFrequency(tc.in)
		if got != tc.want {
			t.Errorf("parseFrequency(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ics")
	if err := os.WriteFile(path, []byte(testICS), 0644); err != nil {
		t.Fatal(err)
	}
	yc, err := Load(path, "File Test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(yc.Events) != 4 {
		t.Errorf("got %d events, want 4", len(yc.Events))
	}
}

func TestLoadFileMissing(t *testing.T) {
	_, err := Load("/nonexistent/path.ics", "Missing")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDedup(t *testing.T) {
	events := []Event{
		{Key: "a", Month: time.January},
		{Key: "a", Month: time.January},
		{Key: "a", Month: time.February},
	}
	got := dedup(events)
	if len(got) != 2 {
		t.Errorf("dedup: got %d, want 2", len(got))
	}
}

func TestExpandMonthly(t *testing.T) {
	ev := Event{Key: "test", Month: time.January, Frequency: "monthly"}
	expanded := expandRecurring(ev)
	if len(expanded) != 12 {
		t.Errorf("monthly expansion: got %d, want 12", len(expanded))
	}
}

func TestExpandWeekly(t *testing.T) {
	ev := Event{Key: "test", Month: time.January, Frequency: "weekly"}
	expanded := expandRecurring(ev)
	if len(expanded) != 12 {
		t.Errorf("weekly expansion: got %d, want 12", len(expanded))
	}
}

func TestExpandOneTime(t *testing.T) {
	ev := Event{Key: "test", Month: time.June, Frequency: ""}
	expanded := expandRecurring(ev)
	if len(expanded) != 1 {
		t.Errorf("one-time expansion: got %d, want 1", len(expanded))
	}
}

func TestParseICalDate(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"20260115T090000Z", false},
		{"20260115T090000", false},
		{"20260115", false},
		{"not-a-date", true},
	}
	for _, tc := range tests {
		_, err := parseICalDate(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseICalDate(%q): err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestParseInvalidICS(t *testing.T) {
	_, err := Parse(strings.NewReader("not valid ical"), "Bad", "")
	if err == nil {
		t.Error("expected error for invalid iCal")
	}
}

func TestEventMissingSummary(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260115T090000Z
END:VEVENT
END:VCALENDAR`
	yc, err := Parse(strings.NewReader(ics), "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(yc.Events) != 0 {
		t.Errorf("expected 0 events for missing summary, got %d", len(yc.Events))
	}
}

func TestEventMissingDtstart(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
SUMMARY:Test Event
END:VEVENT
END:VCALENDAR`
	yc, err := Parse(strings.NewReader(ics), "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(yc.Events) != 0 {
		t.Errorf("expected 0 events for missing dtstart, got %d", len(yc.Events))
	}
}
