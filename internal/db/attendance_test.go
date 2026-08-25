package db

import (
	"testing"
	"time"

	"paylash/internal/models"
)

// These exercise computeAttendanceStatus/isScheduledWorkday directly — pure
// Go logic with no DB dependency, unlike the round-trip CheckIn/CheckOut
// behavior (needs TEST_DATABASE_URL, see integration_test.go). This is
// exactly the "was the employee late/early" math the whole feature hinges
// on, so it's worth pinning down without needing a live Postgres.

func TestComputeAttendanceStatusOnTime(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local) // a Monday
	r := models.AttendanceRecord{
		CheckInAt: day.Add(9 * time.Hour), // 09:00, exactly on time
		WorkDate:  "2026-03-02",
		ExpectedStartMin: 540, // 09:00
		ExpectedEndMin:   1080, // 18:00
		GraceMinutes:     10,
		IsWorkday:        true,
	}
	computeAttendanceStatus(&r)
	if r.IsLate {
		t.Fatalf("check-in exactly at expected_start must not be late, got late by %d min", r.LateMinutes)
	}
}

func TestComputeAttendanceStatusLateBeyondGrace(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	r := models.AttendanceRecord{
		CheckInAt:        day.Add(9*time.Hour + 20*time.Minute), // 09:20
		ExpectedStartMin: 540,                                   // 09:00
		ExpectedEndMin:   1080,
		GraceMinutes:     10, // grace covers up to 09:10
		IsWorkday:        true,
	}
	computeAttendanceStatus(&r)
	if !r.IsLate {
		t.Fatal("check-in 20 minutes past expected_start with a 10-minute grace must be late")
	}
	// Late minutes are measured from expected_start itself, not the grace
	// deadline, so a report of "23 minutes late" matches what the employee
	// actually experiences (the grace window only decides the threshold).
	if r.LateMinutes != 20 {
		t.Fatalf("expected 20 late minutes, got %d", r.LateMinutes)
	}
}

func TestComputeAttendanceStatusWithinGrace(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	r := models.AttendanceRecord{
		CheckInAt:        day.Add(9*time.Hour + 5*time.Minute), // 09:05
		ExpectedStartMin: 540,
		ExpectedEndMin:   1080,
		GraceMinutes:     10,
		IsWorkday:        true,
	}
	computeAttendanceStatus(&r)
	if r.IsLate {
		t.Fatal("check-in within the grace period must not be flagged late")
	}
}

func TestComputeAttendanceStatusEarlyLeave(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	checkOut := day.Add(17 * time.Hour) // 17:00
	r := models.AttendanceRecord{
		CheckInAt:        day.Add(9 * time.Hour),
		CheckOutAt:       &checkOut,
		ExpectedStartMin: 540,
		ExpectedEndMin:   1080, // 18:00
		GraceMinutes:     10,
		IsWorkday:        true,
	}
	computeAttendanceStatus(&r)
	if !r.IsEarlyLeave {
		t.Fatal("checking out an hour before expected_end must be flagged early leave")
	}
	if r.EarlyLeaveMinutes != 60 {
		t.Fatalf("expected 60 early-leave minutes, got %d", r.EarlyLeaveMinutes)
	}
	if r.WorkedMinutes != 8*60 {
		t.Fatalf("expected 480 worked minutes, got %d", r.WorkedMinutes)
	}
}

func TestComputeAttendanceStatusNonWorkdayNeverFlagged(t *testing.T) {
	day := time.Date(2026, 3, 7, 0, 0, 0, 0, time.Local) // a Saturday
	r := models.AttendanceRecord{
		CheckInAt:        day.Add(15 * time.Hour), // 15:00, way past any "expected" start
		ExpectedStartMin: 540,
		ExpectedEndMin:   1080,
		GraceMinutes:     10,
		IsWorkday:        false, // snapshotted as a non-workday at check-in time
	}
	computeAttendanceStatus(&r)
	if r.IsLate || r.IsEarlyLeave {
		t.Fatal("a record snapshotted as a non-workday must never be flagged late or early, regardless of the time")
	}
}

func TestComputeAttendanceStatusNeverReportsNegativeWork(t *testing.T) {
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	checkIn := day.Add(9 * time.Hour)
	// Check-out stamped a moment BEFORE check-in — what a backwards host
	// clock step between the two requests produces.
	checkOut := checkIn.Add(-40 * time.Millisecond)
	r := models.AttendanceRecord{
		CheckInAt: checkIn, CheckOutAt: &checkOut,
		ExpectedStartMin: 540, ExpectedEndMin: 1080, GraceMinutes: 10, IsWorkday: true,
	}
	computeAttendanceStatus(&r)
	if r.WorkedMinutes < 0 {
		t.Fatalf("worked minutes must never be negative, got %d", r.WorkedMinutes)
	}
}

// Regression: a timestamptz read back from Postgres arrives in the
// CONNECTION's zone (UTC by default), not the server's local zone. Lateness
// must still be judged against the local office clock — reading the
// timestamp's own date/hour parts instead would shift every verdict by the
// UTC offset (a UTC+5 office had 09:00 arrivals measured against 14:00).
func TestComputeAttendanceStatusUsesLocalZoneNotTimestampZone(t *testing.T) {
	// 09:20 local, with the value expressed in UTC the way lib/pq hands it back.
	localCheckIn := time.Date(2026, 3, 2, 9, 20, 0, 0, time.Local)
	r := models.AttendanceRecord{
		CheckInAt:        localCheckIn.UTC(),
		ExpectedStartMin: 540, // 09:00 local
		ExpectedEndMin:   1080,
		GraceMinutes:     10,
		IsWorkday:        true,
	}
	computeAttendanceStatus(&r)
	if !r.IsLate {
		t.Fatal("a 09:20 local check-in must be late even when the timestamp is carried in UTC")
	}
	if r.LateMinutes != 20 {
		t.Fatalf("expected 20 late minutes regardless of the timestamp's zone, got %d", r.LateMinutes)
	}
}

func TestCountExpectedWorkdays(t *testing.T) {
	monFri := models.AttendanceSchedule{Workdays: []int{1, 2, 3, 4, 5}}
	// 2026-03-02 is a Monday; 2026-03-08 the Sunday that ends that week.
	if got := CountExpectedWorkdays(monFri, "2026-03-02", "2026-03-08"); got != 5 {
		t.Errorf("a full Mon-Sun week with a Mon-Fri schedule = 5 workdays, got %d", got)
	}
	// Range boundaries are inclusive on both ends.
	if got := CountExpectedWorkdays(monFri, "2026-03-02", "2026-03-02"); got != 1 {
		t.Errorf("a single Monday = 1 workday, got %d", got)
	}
	if got := CountExpectedWorkdays(monFri, "2026-03-07", "2026-03-08"); got != 0 {
		t.Errorf("a Sat-Sun range with a Mon-Fri schedule = 0 workdays, got %d", got)
	}
	// A six-day schedule including Saturday must count that Saturday.
	monSat := models.AttendanceSchedule{Workdays: []int{1, 2, 3, 4, 5, 6}}
	if got := CountExpectedWorkdays(monSat, "2026-03-02", "2026-03-08"); got != 6 {
		t.Errorf("a Mon-Sat schedule over a full week = 6 workdays, got %d", got)
	}
	// A whole month, so an off-by-one in the loop bound would show up.
	if got := CountExpectedWorkdays(monFri, "2026-03-01", "2026-03-31"); got != 22 {
		t.Errorf("March 2026 has 22 Mon-Fri days, got %d", got)
	}
	// Malformed input must not panic or loop forever.
	if got := CountExpectedWorkdays(monFri, "not-a-date", "2026-03-31"); got != 0 {
		t.Errorf("an unparseable date must yield 0, got %d", got)
	}
}

func TestIsScheduledWorkday(t *testing.T) {
	sched := models.AttendanceSchedule{Workdays: []int{1, 2, 3, 4, 5}} // Mon-Fri
	cases := []struct {
		day  time.Weekday
		want bool
	}{
		{time.Monday, true},
		{time.Friday, true},
		{time.Saturday, false},
		{time.Sunday, false},
	}
	for _, c := range cases {
		if got := isScheduledWorkday(sched, c.day); got != c.want {
			t.Errorf("isScheduledWorkday(%s) = %v, want %v", c.day, got, c.want)
		}
	}
}
