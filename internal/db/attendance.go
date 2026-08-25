package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"paylash/internal/models"
)

// defaultAttendanceScheduleJSON is what a fresh install (or a settings row
// that was never written) gets: 09:00-18:00, 10 minutes grace, Mon-Fri.
const defaultAttendanceScheduleJSON = `{"start_min":540,"end_min":1080,"grace_minutes":10,"workdays":[1,2,3,4,5]}`

// GetAttendanceSchedule returns the single company-wide work schedule,
// stored under the 'attendance_schedule' settings key — same
// settings-table pattern AdminGetPublicQuota already uses, rather than a
// dedicated one-row table.
func (d *DB) GetAttendanceSchedule() (models.AttendanceSchedule, error) {
	val, err := d.GetSetting("attendance_schedule")
	if err != nil || val == "" {
		val = defaultAttendanceScheduleJSON
	}
	var s models.AttendanceSchedule
	if err := json.Unmarshal([]byte(val), &s); err != nil || len(s.Workdays) == 0 {
		json.Unmarshal([]byte(defaultAttendanceScheduleJSON), &s)
	}
	return s, nil
}

func (d *DB) SetAttendanceSchedule(s models.AttendanceSchedule) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return d.SetSetting("attendance_schedule", string(b))
}

// isScheduledWorkday reports whether weekday (time.Weekday, 0=Sunday) is one
// of the schedule's configured working days.
func isScheduledWorkday(s models.AttendanceSchedule, weekday time.Weekday) bool {
	for _, w := range s.Workdays {
		if w == int(weekday) {
			return true
		}
	}
	return false
}

const attendanceRecordCols = `id, user_id, work_date::text, check_in_at, check_out_at, expected_start_min, expected_end_min, grace_minutes, is_workday, needs_review, notes, created_at, updated_at`

func scanAttendanceRecord(row scanner) (*models.AttendanceRecord, error) {
	var r models.AttendanceRecord
	var checkOut sql.NullTime
	if err := row.Scan(&r.ID, &r.UserID, &r.WorkDate, &r.CheckInAt, &checkOut, &r.ExpectedStartMin, &r.ExpectedEndMin, &r.GraceMinutes, &r.IsWorkday, &r.NeedsReview, &r.Notes, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	if checkOut.Valid {
		t := checkOut.Time
		r.CheckOutAt = &t
	}
	computeAttendanceStatus(&r)
	return &r, nil
}

// computeAttendanceStatus fills the derived (never-stored) fields on r from
// its own snapshotted schedule columns. A non-workday record is never
// flagged late/early — see is_workday's comment in db.go.
func computeAttendanceStatus(r *models.AttendanceRecord) {
	if !r.IsWorkday {
		return
	}
	// Anchor to midnight in the SERVER's local zone (set via TZ — see the
	// app service in docker-compose.yml), NOT in whatever zone the timestamp
	// happens to carry. A timestamptz read back from Postgres arrives in the
	// connection's zone, which is UTC by default: taking its raw date/hour
	// parts would measure a 09:00 start against UTC midnight, so in a UTC+5
	// office every arrival would be judged against 14:00 local.
	checkIn := r.CheckInAt.In(time.Local)
	dayStart := time.Date(checkIn.Year(), checkIn.Month(), checkIn.Day(), 0, 0, 0, 0, time.Local)
	expectedStart := dayStart.Add(time.Duration(r.ExpectedStartMin) * time.Minute)
	graceDeadline := expectedStart.Add(time.Duration(r.GraceMinutes) * time.Minute)
	if checkIn.After(graceDeadline) {
		r.IsLate = true
		r.LateMinutes = int(checkIn.Sub(expectedStart).Minutes())
	}
	if r.CheckOutAt != nil {
		// Clamped at zero: a check-out stamped fractionally BEFORE its
		// check-in is possible if the host clock steps backwards between the
		// two requests (observed in a container during testing), and a
		// negative "worked" duration would render as nonsense like "-1h 30m".
		r.WorkedMinutes = int(r.CheckOutAt.Sub(r.CheckInAt).Minutes())
		if r.WorkedMinutes < 0 {
			r.WorkedMinutes = 0
		}
		expectedEnd := dayStart.Add(time.Duration(r.ExpectedEndMin) * time.Minute)
		if r.CheckOutAt.Before(expectedEnd) {
			r.IsEarlyLeave = true
			r.EarlyLeaveMinutes = int(expectedEnd.Sub(*r.CheckOutAt).Minutes())
		}
	}
}

// CheckIn records userID's arrival for today (the server's local calendar
// date — see attendance_records.work_date's comment) using the schedule
// snapshot the caller already fetched. Returns an error if userID already
// has a record for today (ON CONFLICT DO NOTHING + no returned row) —
// double check-in is rejected, not silently overwritten.
func (d *DB) CheckIn(userID int, sched models.AttendanceSchedule) (*models.AttendanceRecord, error) {
	now := time.Now()
	workDate := now.Format("2006-01-02")
	isWorkday := isScheduledWorkday(sched, now.Weekday())
	row := d.QueryRow(
		`INSERT INTO attendance_records (user_id, work_date, check_in_at, expected_start_min, expected_end_min, grace_minutes, is_workday)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (user_id, work_date) DO NOTHING
		 RETURNING `+attendanceRecordCols,
		userID, workDate, now, sched.StartMin, sched.EndMin, sched.GraceMinutes, isWorkday,
	)
	r, err := scanAttendanceRecord(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("already checked in today")
	}
	return r, err
}

// CheckOut sets check_out_at on today's record for userID. Errors if there
// is no open record for today (never checked in, or already checked out).
func (d *DB) CheckOut(userID int) (*models.AttendanceRecord, error) {
	now := time.Now()
	workDate := now.Format("2006-01-02")
	row := d.QueryRow(
		`UPDATE attendance_records SET check_out_at = $3, updated_at = NOW()
		 WHERE user_id = $1 AND work_date = $2 AND check_out_at IS NULL
		 RETURNING `+attendanceRecordCols,
		userID, workDate, now,
	)
	r, err := scanAttendanceRecord(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("not checked in")
	}
	return r, err
}

// GetTodayAttendance returns userID's record for today, or nil (not an
// error) if they haven't checked in yet.
func (d *DB) GetTodayAttendance(userID int) (*models.AttendanceRecord, error) {
	return d.getAttendanceByUserDate(userID, time.Now().Format("2006-01-02"))
}

func (d *DB) getAttendanceByUserDate(userID int, workDate string) (*models.AttendanceRecord, error) {
	row := d.QueryRow(`SELECT `+attendanceRecordCols+` FROM attendance_records WHERE user_id = $1 AND work_date = $2`, userID, workDate)
	r, err := scanAttendanceRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// ListMyAttendance returns userID's own records in [from, to] (YYYY-MM-DD,
// inclusive), newest first — backs the personal history page.
func (d *DB) ListMyAttendance(userID int, from, to string) ([]models.AttendanceRecord, error) {
	rows, err := d.Query(
		`SELECT `+attendanceRecordCols+` FROM attendance_records
		 WHERE user_id = $1 AND work_date BETWEEN $2 AND $3
		 ORDER BY work_date DESC`,
		userID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.AttendanceRecord
	for rows.Next() {
		r, err := scanAttendanceRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// ListAttendance returns every employee's records in [from, to], newest
// first, joined with display info — backs the admin/manager table and CSV
// export. userID narrows to one employee when non-nil.
func (d *DB) ListAttendance(from, to string, userID *int) ([]models.AttendanceView, error) {
	query := `SELECT a.id, a.user_id, a.work_date::text, a.check_in_at, a.check_out_at, a.expected_start_min, a.expected_end_min, a.grace_minutes, a.is_workday, a.needs_review, a.notes, a.created_at, a.updated_at,
	                  u.username, COALESCE(u.display_name, u.username, ''), u.avatar_url
	           FROM attendance_records a JOIN users u ON u.id = a.user_id
	           WHERE a.work_date BETWEEN $1 AND $2`
	args := []any{from, to}
	if userID != nil {
		query += ` AND a.user_id = $3`
		args = append(args, *userID)
	}
	query += ` ORDER BY a.work_date DESC, u.username`

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.AttendanceView
	for rows.Next() {
		var v models.AttendanceView
		var checkOut sql.NullTime
		if err := rows.Scan(&v.ID, &v.UserID, &v.WorkDate, &v.CheckInAt, &checkOut, &v.ExpectedStartMin, &v.ExpectedEndMin, &v.GraceMinutes, &v.IsWorkday, &v.NeedsReview, &v.Notes, &v.CreatedAt, &v.UpdatedAt,
			&v.Username, &v.DisplayName, &v.AvatarURL); err != nil {
			return nil, err
		}
		if checkOut.Valid {
			t := checkOut.Time
			v.CheckOutAt = &t
		}
		computeAttendanceStatus(&v.AttendanceRecord)
		list = append(list, v)
	}
	return list, rows.Err()
}

// GetAttendanceAnalytics aggregates [from, to] for the admin/manager
// dashboard. Late/early are computed in Go (computeAttendanceStatus) rather
// than reimplemented in SQL, so the two can never disagree — fine at this
// app's scale (one company's employees × workdays, never a huge row count).
func (d *DB) GetAttendanceAnalytics(from, to string) (*models.AttendanceAnalytics, error) {
	rows, err := d.Query(`SELECT `+attendanceRecordCols+` FROM attendance_records WHERE work_date BETWEEN $1 AND $2 ORDER BY work_date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	a := &models.AttendanceAnalytics{}
	dailyIdx := make(map[string]int)
	totalWorked, workedCount := 0, 0
	for rows.Next() {
		r, err := scanAttendanceRecord(rows)
		if err != nil {
			return nil, err
		}
		a.TotalRecords++
		if r.NeedsReview {
			a.NeedsReviewCount++
		}
		if r.IsLate {
			a.LateCount++
		}
		if r.IsEarlyLeave {
			a.EarlyLeaveCount++
		}
		if r.WorkedMinutes > 0 {
			totalWorked += r.WorkedMinutes
			workedCount++
		}
		idx, ok := dailyIdx[r.WorkDate]
		if !ok {
			idx = len(a.Daily)
			dailyIdx[r.WorkDate] = idx
			a.Daily = append(a.Daily, models.AttendanceDailyPoint{Date: r.WorkDate})
		}
		a.Daily[idx].Total++
		if r.IsLate {
			a.Daily[idx].LateCount++
		}
		if r.IsEarlyLeave {
			a.Daily[idx].EarlyLeaveCount++
		}
		if r.NeedsReview {
			a.Daily[idx].NeedsReviewCount++
		}
	}
	if workedCount > 0 {
		a.AvgWorkedMinutes = totalWorked / workedCount
	}
	return a, rows.Err()
}

// CountExpectedWorkdays counts days in [from, to] (inclusive, YYYY-MM-DD)
// whose weekday is in sched's workday set. Uses the CURRENT schedule
// rather than each record's snapshot on purpose: this answers "how many
// days SHOULD someone have come this month", which is a property of the
// range, not of records that may not exist (an absent employee has no
// snapshot to read).
func CountExpectedWorkdays(sched models.AttendanceSchedule, from, to string) int {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return 0
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return 0
	}
	n := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if isScheduledWorkday(sched, d.Weekday()) {
			n++
		}
	}
	return n
}

// GetAttendanceEmployeeSummaries aggregates [from, to] per employee for the
// monthly analytics table. LEFT JOIN from users (not from records) so an
// employee who never checked in still comes back as an all-zero row — that
// absence is the single most useful thing this table shows. Aggregation
// happens in Go via computeAttendanceStatus for the same reason
// GetAttendanceAnalytics does it: one definition of "late", never two.
func (d *DB) GetAttendanceEmployeeSummaries(from, to string) ([]models.AttendanceEmployeeSummary, error) {
	sched, err := d.GetAttendanceSchedule()
	if err != nil {
		return nil, err
	}
	expected := CountExpectedWorkdays(sched, from, to)

	rows, err := d.Query(
		`SELECT u.id, u.username, COALESCE(u.display_name, u.username, ''), u.avatar_url,
		        a.work_date::text, a.check_in_at, a.check_out_at,
		        a.expected_start_min, a.expected_end_min, a.grace_minutes, a.is_workday, a.needs_review
		 FROM users u
		 LEFT JOIN attendance_records a
		   ON a.user_id = u.id AND a.work_date BETWEEN $1 AND $2
		 ORDER BY u.username, a.work_date`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.AttendanceEmployeeSummary
	byUser := make(map[int]int) // user id -> index into list
	checkInTotals := make(map[int]int)
	checkInCounts := make(map[int]int)
	workedCounts := make(map[int]int)

	for rows.Next() {
		var uid int
		var username, displayName, avatarURL string
		var workDate sql.NullString
		var checkIn, checkOut sql.NullTime
		var startMin, endMin, grace sql.NullInt64
		var isWorkday, needsReview sql.NullBool
		if err := rows.Scan(&uid, &username, &displayName, &avatarURL,
			&workDate, &checkIn, &checkOut, &startMin, &endMin, &grace, &isWorkday, &needsReview); err != nil {
			return nil, err
		}

		idx, ok := byUser[uid]
		if !ok {
			idx = len(list)
			byUser[uid] = idx
			list = append(list, models.AttendanceEmployeeSummary{
				UserID: uid, Username: username, DisplayName: displayName, AvatarURL: avatarURL,
				ExpectedWorkdays: expected, AvgCheckInMinutes: -1, PunctualityPct: -1,
			})
		}
		if !workDate.Valid || !checkIn.Valid {
			continue // employee with no records in range — the all-zero row above is the answer
		}

		rec := models.AttendanceRecord{
			WorkDate:         workDate.String,
			CheckInAt:        checkIn.Time,
			ExpectedStartMin: int(startMin.Int64),
			ExpectedEndMin:   int(endMin.Int64),
			GraceMinutes:     int(grace.Int64),
			IsWorkday:        isWorkday.Bool,
			NeedsReview:      needsReview.Bool,
		}
		if checkOut.Valid {
			t := checkOut.Time
			rec.CheckOutAt = &t
		}
		computeAttendanceStatus(&rec)

		s := &list[idx]
		s.DaysPresent++
		if rec.IsLate {
			s.LateCount++
			s.TotalLateMinutes += rec.LateMinutes
		}
		if rec.IsEarlyLeave {
			s.EarlyLeaveCount++
			s.TotalEarlyMinutes += rec.EarlyLeaveMinutes
		}
		if rec.NeedsReview {
			s.NeedsReviewCount++
		}
		if rec.WorkedMinutes > 0 {
			s.TotalWorkedMinutes += rec.WorkedMinutes
			workedCounts[uid]++
		}
		// Local zone for the same reason computeAttendanceStatus uses it —
		// Hour()/Minute() are zone-dependent, and this average is meant to
		// read as an office wall-clock time.
		localIn := rec.CheckInAt.In(time.Local)
		checkInTotals[uid] += localIn.Hour()*60 + localIn.Minute()
		checkInCounts[uid]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range list {
		uid := list[i].UserID
		if n := workedCounts[uid]; n > 0 {
			list[i].AvgWorkedMinutes = list[i].TotalWorkedMinutes / n
		}
		if n := checkInCounts[uid]; n > 0 {
			list[i].AvgCheckInMinutes = checkInTotals[uid] / n
		}
		if list[i].DaysPresent > 0 {
			onTime := list[i].DaysPresent - list[i].LateCount
			list[i].PunctualityPct = onTime * 100 / list[i].DaysPresent
		}
	}
	return list, nil
}

// UpdateAttendanceRecord is the admin-only correction path (e.g. resolving
// a needs_review row, or fixing a mistaken time) — always clears
// needs_review, since editing the record is how that flag gets resolved.
func (d *DB) UpdateAttendanceRecord(id int, checkInAt time.Time, checkOutAt *time.Time, notes string) error {
	_, err := d.Exec(
		`UPDATE attendance_records SET check_in_at = $2, check_out_at = $3, notes = $4, needs_review = FALSE, updated_at = NOW() WHERE id = $1`,
		id, checkInAt, checkOutAt, notes,
	)
	return err
}

func (d *DB) GetAttendanceRecordByID(id int) (*models.AttendanceRecord, error) {
	row := d.QueryRow(`SELECT `+attendanceRecordCols+` FROM attendance_records WHERE id = $1`, id)
	r, err := scanAttendanceRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// FlagMissingCheckouts marks needs_review = TRUE for every record whose
// check_out_at is still NULL and whose work_date has already fully passed —
// i.e. a work day that ended with the employee never checking out. Never
// guesses/fills a checkout time (deliberate product decision, see db.go) —
// this only raises a flag for an admin to resolve via UpdateAttendanceRecord.
// Run nightly by internal/janitor.
func (d *DB) FlagMissingCheckouts() (int, error) {
	// "Today" comes from the APP's local zone, not Postgres's CURRENT_DATE:
	// work_date is written from time.Now() here, so comparing it against a
	// date the database computes in whatever zone its own config happens to
	// use would disagree for a few hours around midnight whenever the two
	// differ (an existing data volume keeps the timezone it was initialised
	// with, regardless of what the container's TZ says today).
	today := time.Now().Format("2006-01-02")
	res, err := d.Exec(
		`UPDATE attendance_records SET needs_review = TRUE, updated_at = NOW()
		 WHERE check_out_at IS NULL AND needs_review = FALSE AND work_date < $1`,
		today,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
