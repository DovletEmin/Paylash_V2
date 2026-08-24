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
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // a Monday
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
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
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
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
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
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
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
	day := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC) // a Saturday
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
