package transport

import (
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

type Event = domain.Event

func escapeICS(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", ";", "\\;", ",", "\\,").Replace(value)
}

// RFC 5545 folds lines at 75 octets, without cutting through a UTF-8 character.
func foldICS(line string) string {
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = 74
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}

func calendarICS(events []Event, now time.Time) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Orbita//Space Calendar//RU", "CALSCALE:GREGORIAN", "X-WR-CALNAME:Орбита"}
	for _, e := range events {
		date := strings.ReplaceAll(e.Date, "-", "")
		lines = append(lines, "BEGIN:VEVENT", "UID:"+escapeICS(e.ID)+"@orbita.local", "DTSTAMP:"+now.UTC().Format("20060102T150405Z"))
		if e.AllDay {
			d, _ := time.Parse("2006-01-02", e.Date)
			lines = append(lines, "DTSTART;VALUE=DATE:"+date, "DTEND;VALUE=DATE:"+d.AddDate(0, 0, 1).Format("20060102"))
		} else {
			// Floating times follow the importing calendar's local time zone.
			lines = append(lines, "DTSTART:"+date+"T"+strings.ReplaceAll(e.Start, ":", "")+"00", "DTEND:"+date+"T"+strings.ReplaceAll(e.End, ":", "")+"00")
		}
		lines = append(lines, "SUMMARY:"+escapeICS(e.Title), "DESCRIPTION:"+escapeICS(e.Description), "LOCATION:"+escapeICS(e.Location), "CATEGORIES:"+e.Category, "END:VEVENT")
	}
	lines = append(lines, "END:VCALENDAR")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(foldICS(line))
	}
	return b.String()
}
