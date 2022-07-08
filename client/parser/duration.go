package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Duration represents an ISO8601 Duration
// https://en.wikipedia.org/wiki/ISO_8601#Durations
type Duration struct {
	Y int
	M int
	W int
	D int
	// Time Component
	TH int
	TM int
	TS int
}

var pattern = regexp.MustCompile(`^P((?P<year>\d+)Y)?((?P<month>\d+)M)?((?P<week>\d+)W)?((?P<day>\d+)D)?(T((?P<hour>\d+)H)?((?P<minute>\d+)M)?((?P<second>\d+)S)?)?$`)

// ParseISO8601 parses an ISO8601 duration string.
func ParseISO8601(from string) (Duration, error) {
	var match []string
	var d Duration

	if pattern.MatchString(from) {
		match = pattern.FindStringSubmatch(from)
	} else {
		return d, errors.New("could not parse duration string")
	}

	for i, name := range pattern.SubexpNames() {
		part := match[i]
		if i == 0 || name == "" || part == "" {
			continue
		}

		val, err := strconv.Atoi(part)
		if err != nil {
			return d, err
		}
		switch name {
		case "year":
			d.Y = val
		case "month":
			d.M = val
		case "week":
			d.W = val
		case "day":
			d.D = val
		case "hour":
			d.TH = val
		case "minute":
			d.TM = val
		case "second":
			d.TS = val
		default:
			return d, fmt.Errorf("unknown field %s", name)
		}
	}

	return d, nil
}

// IsZero reports whether d represents the zero duration, P0D.
func (d Duration) IsZero() bool {
	return d.Y == 0 && d.M == 0 && d.W == 0 && d.D == 0 && d.TH == 0 && d.TM == 0 && d.TS == 0
}

// HasTimePart returns true if the time part of the duration is non-zero.
func (d Duration) HasTimePart() bool {
	return d.TH > 0 || d.TM > 0 || d.TS > 0
}

// Shift returns a time.Time, shifted by the duration from the given start.
//
// NB: Shift uses time.AddDate for years, months, weeks, and days, and so
// shares its limitations. In particular, shifting by months is not recommended
// unless the start date is before the 28th of the month. Otherwise, dates will
// roll over, e.g. Aug 31 + P1M = Oct 1.
//
// Week and Day values will be combined as W*7 + D.
func (d Duration) Shift(t time.Time) time.Time {
	if d.Y != 0 || d.M != 0 || d.W != 0 || d.D != 0 {
		days := d.W*7 + d.D
		t = t.AddDate(d.Y, d.M, days)
	}
	t = t.Add(d.timeDuration())
	return t
}

func (d Duration) timeDuration() time.Duration {
	var dur time.Duration
	dur = dur + (time.Duration(d.TH) * time.Hour)
	dur = dur + (time.Duration(d.TM) * time.Minute)
	dur = dur + (time.Duration(d.TS) * time.Second)
	return dur
}
