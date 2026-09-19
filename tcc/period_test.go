package tcc

import (
	"testing"
	"time"
)

func TestPeriodConversions(t *testing.T) {
	period, err := PeriodFromClock(18, 30)
	if err != nil {
		t.Fatal(err)
	}
	if period != 74 {
		t.Fatalf("expected period 74; got %d", period)
	}
	hour, minute, err := period.Clock()
	if err != nil {
		t.Fatal(err)
	}
	if hour != 18 || minute != 30 {
		t.Fatalf("expected 18:30; got %02d:%02d", hour, minute)
	}
	if period.Index() != 74 {
		t.Fatalf("expected raw index 74; got %d", period.Index())
	}
}

func TestPeriodFromTimeRequiresPeriodBoundary(t *testing.T) {
	_, err := PeriodFromTime(time.Date(2026, time.May, 2, 18, 37, 0, 0, time.Local))
	if err == nil {
		t.Fatal("expected non-boundary time to fail")
	}
}

func TestPeriodContainingTime(t *testing.T) {
	period := PeriodContainingTime(time.Date(2026, time.May, 2, 18, 37, 59, 0, time.Local))
	if period != 74 {
		t.Fatalf("expected period 74; got %d", period)
	}
}

func TestPeriodTimeOn(t *testing.T) {
	date := time.Date(2026, time.May, 2, 9, 1, 2, 3, time.FixedZone("test", -4*60*60))
	period, err := PeriodFromClock(18, 30)
	if err != nil {
		t.Fatal(err)
	}
	value, err := period.TimeOn(date)
	if err != nil {
		t.Fatal(err)
	}
	if value.Year() != 2026 || value.Month() != time.May || value.Day() != 2 {
		t.Fatalf("unexpected date: %s", value)
	}
	if value.Hour() != 18 || value.Minute() != 30 || value.Second() != 0 || value.Nanosecond() != 0 {
		t.Fatalf("unexpected clock time: %s", value)
	}
	if value.Location() != date.Location() {
		t.Fatalf("expected location to be preserved")
	}
}

func TestNewPeriodRejectsOutOfRangeIndex(t *testing.T) {
	if _, err := NewPeriod(-1); err == nil {
		t.Fatal("expected negative period to fail")
	}
	if _, err := NewPeriod(PeriodsPerDay); err == nil {
		t.Fatal("expected period past end of day to fail")
	}
}
