package tcc

import (
	"fmt"
	"time"
)

const (
	// PeriodDuration is the length of one Total Connect Comfort schedule period.
	PeriodDuration = 15 * time.Minute

	// PeriodsPerDay is the number of Total Connect Comfort schedule periods in a day.
	PeriodsPerDay = 24 * int(time.Hour/PeriodDuration)
)

// Period represents a 15-minute schedule period in a day.
//
// Total Connect Comfort encodes these periods as integers from 0 to 95, where
// 0 is 00:00, 1 is 00:15, and 95 is 23:45.
type Period int

// NewPeriod returns a Period for a raw Total Connect Comfort period index.
func NewPeriod(index int) (Period, error) {
	if index < 0 || index >= PeriodsPerDay {
		return 0, fmt.Errorf("period index %d out of range [0,%d]", index, PeriodsPerDay-1)
	}
	return Period(index), nil
}

// PeriodFromClock returns the period that starts at hour:minute.
func PeriodFromClock(hour, minute int) (Period, error) {
	if hour < 0 || hour > 23 {
		return 0, fmt.Errorf("hour %d out of range [0,23]", hour)
	}
	if minute < 0 || minute > 59 {
		return 0, fmt.Errorf("minute %d out of range [0,59]", minute)
	}
	if minute%int(PeriodDuration/time.Minute) != 0 {
		return 0, fmt.Errorf("minute %d is not on a %s boundary", minute, PeriodDuration)
	}
	return Period(hour*int(time.Hour/PeriodDuration) + minute/int(PeriodDuration/time.Minute)), nil
}

// PeriodFromTime returns the period that starts at t's clock time.
func PeriodFromTime(t time.Time) (Period, error) {
	if t.Second() != 0 || t.Nanosecond() != 0 {
		return 0, fmt.Errorf("time %s is not on a %s boundary", t.Format("15:04:05.999999999"), PeriodDuration)
	}
	return PeriodFromClock(t.Hour(), t.Minute())
}

// PeriodContainingTime returns the period containing t's clock time.
func PeriodContainingTime(t time.Time) Period {
	minutes := t.Hour()*60 + t.Minute()
	return Period(minutes / int(PeriodDuration/time.Minute))
}

// Index returns the raw Total Connect Comfort period index.
func (p Period) Index() int {
	return int(p)
}

// Valid reports whether p is a valid Total Connect Comfort period.
func (p Period) Valid() bool {
	return p >= 0 && p < Period(PeriodsPerDay)
}

// Duration returns the duration since midnight at the start of p.
func (p Period) Duration() (time.Duration, error) {
	if !p.Valid() {
		return 0, fmt.Errorf("period index %d out of range [0,%d]", p, PeriodsPerDay-1)
	}
	return time.Duration(p) * PeriodDuration, nil
}

// Clock returns the hour and minute at the start of p.
func (p Period) Clock() (hour, minute int, err error) {
	d, err := p.Duration()
	if err != nil {
		return 0, 0, err
	}
	totalMinutes := int(d / time.Minute)
	return totalMinutes / 60, totalMinutes % 60, nil
}

// TimeOn returns a time on the same date and in the same location as date,
// positioned at the start of p.
func (p Period) TimeOn(date time.Time) (time.Time, error) {
	hour, minute, err := p.Clock()
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location()), nil
}
