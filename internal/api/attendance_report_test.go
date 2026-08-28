package api

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"paylash/internal/models"
)

// roundTrip builds the workbook and re-opens it, so every assertion below is
// made against the bytes a browser would actually download — not against the
// in-memory structs that produced them.
func roundTrip(t *testing.T, from, to string, sched models.AttendanceSchedule,
	recs []models.AttendanceView, sums []models.AttendanceEmployeeSummary) *excelize.File {
	t.Helper()
	f, err := buildAttendanceWorkbook(from, to, sched, recs, sums)
	if err != nil {
		t.Fatalf("buildAttendanceWorkbook: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()
	out, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { out.Close() })
	return out
}

func testSchedule() models.AttendanceSchedule {
	return models.AttendanceSchedule{StartMin: 540, EndMin: 1080, GraceMinutes: 10, Workdays: []int{1, 2, 3, 4, 5}}
}

// A late arrival and a punctual one, both with a checkout, plus one record
// that was never closed — the three shapes the detail sheet has to render
// differently.
func testRecords() []models.AttendanceView {
	mk := func(date, name, user string, inH, inM, outH, outM int, closed bool) models.AttendanceView {
		d, _ := time.ParseInLocation("2006-01-02", date, time.Local)
		in := time.Date(d.Year(), d.Month(), d.Day(), inH, inM, 0, 0, time.Local)
		v := models.AttendanceView{
			AttendanceRecord: models.AttendanceRecord{
				WorkDate: date, CheckInAt: in,
				ExpectedStartMin: 540, ExpectedEndMin: 1080, GraceMinutes: 10, IsWorkday: true,
			},
			Username: user, DisplayName: name,
		}
		if closed {
			out := time.Date(d.Year(), d.Month(), d.Day(), outH, outM, 0, 0, time.Local)
			v.CheckOutAt = &out
		}
		return v
	}
	late := mk("2026-08-03", "Иванов Иван", "ivanov", 9, 47, 18, 5, true)
	late.IsLate = true
	late.LateMinutes = 47
	late.WorkedMinutes = 498

	ontime := mk("2026-08-03", "Петрова Анна", "petrova", 8, 55, 18, 10, true)
	ontime.WorkedMinutes = 555

	open := mk("2026-08-04", "Иванов Иван", "ivanov", 9, 2, 0, 0, false)
	open.NeedsReview = true

	return []models.AttendanceView{late, ontime, open}
}

func testSummaries() []models.AttendanceEmployeeSummary {
	return []models.AttendanceEmployeeSummary{
		{
			UserID: 1, Username: "ivanov", DisplayName: "Иванов Иван",
			DaysPresent: 18, ExpectedWorkdays: 21, LateCount: 3, TotalLateMinutes: 95,
			EarlyLeaveCount: 1, TotalEarlyMinutes: 25, NeedsReviewCount: 1,
			TotalWorkedMinutes: 8640, AvgWorkedMinutes: 480,
			AvgCheckInMinutes: 552, PunctualityPct: 83,
		},
		{
			UserID: 2, Username: "petrova", DisplayName: "Петрова Анна",
			DaysPresent: 21, ExpectedWorkdays: 21, TotalWorkedMinutes: 10500, AvgWorkedMinutes: 500,
			AvgCheckInMinutes: 535, PunctualityPct: 100,
		},
		// Never showed up: the -1 sentinels must render as dashes, not as a
		// zero that reads like "arrived at midnight, 0% punctual".
		{UserID: 3, Username: "absent", DisplayName: "Отсутствующий",
			ExpectedWorkdays: 21, AvgCheckInMinutes: -1, PunctualityPct: -1},
	}
}

func TestAttendanceWorkbookStructure(t *testing.T) {
	f := roundTrip(t, "2026-08-01", "2026-08-31", testSchedule(), testRecords(), testSummaries())

	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Сводка" || sheets[1] != "Детализация" {
		t.Fatalf("sheets = %v, want [Сводка Детализация]", sheets)
	}

	// Masthead: period in Russian day-first form, not the ISO the API speaks.
	title, _ := f.GetCellValue("Сводка", "A1")
	if title != "Отчёт по посещаемости" {
		t.Errorf("A1 = %q", title)
	}
	period, _ := f.GetCellValue("Сводка", "A2")
	if want := "Период: 01.08.2026 — 31.08.2026"; !bytes.Contains([]byte(period), []byte(want)) {
		t.Errorf("A2 = %q, want it to contain %q", period, want)
	}
	sched, _ := f.GetCellValue("Сводка", "A3")
	for _, want := range []string{"09:00–18:00", "10 мин", "Пн, Вт, Ср, Чт, Пт"} {
		if !bytes.Contains([]byte(sched), []byte(want)) {
			t.Errorf("A3 = %q, want it to contain %q", sched, want)
		}
	}
}

func TestAttendanceWorkbookDetailValues(t *testing.T) {
	f := roundTrip(t, "2026-08-01", "2026-08-31", testSchedule(), testRecords(), testSummaries())
	const sheet = "Детализация"

	// Headers are Russian and in the documented order.
	wantHeaders := map[string]string{
		"A6": "Дата", "B6": "День недели", "C6": "Сотрудник", "D6": "Логин",
		"E6": "Приход", "F6": "Уход", "G6": "Отработано", "J6": "Статус",
	}
	for cell, want := range wantHeaders {
		got, _ := f.GetCellValue(sheet, cell)
		if got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}

	// Row 7 = the late record. Date/time cells must come back as formatted
	// dates and clock readings, which only happens if they were written as
	// real serial numbers rather than text.
	checks := map[string]string{
		"A7": "03.08.2026",
		"B7": "понедельник",
		"C7": "Иванов Иван",
		"D7": "ivanov",
		"E7": "09:47",
		"F7": "18:05",
		"G7": "8:18", // 498 minutes as elapsed time
		"H7": "47",
	}
	for cell, want := range checks {
		got, _ := f.GetCellValue(sheet, cell)
		if got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
	if status, _ := f.GetCellValue(sheet, "J7"); status != "Опоздание 47 мин" {
		t.Errorf("J7 = %q", status)
	}

	// A punctual day says so rather than leaving the reader to infer it from
	// two blank minute columns.
	if status, _ := f.GetCellValue(sheet, "J8"); status != "Вовремя" {
		t.Errorf("J8 = %q, want Вовремя", status)
	}

	// The never-closed record: a dash, never a 0:00 that reads as "worked
	// nothing", and both open-record facts spelled out.
	if v, _ := f.GetCellValue(sheet, "F9"); v != "—" {
		t.Errorf("F9 = %q, want em dash", v)
	}
	if v, _ := f.GetCellValue(sheet, "G9"); v != "—" {
		t.Errorf("G9 = %q, want em dash", v)
	}
	status, _ := f.GetCellValue(sheet, "J9")
	for _, want := range []string{"Нет отметки об уходе", "Требует проверки"} {
		if !bytes.Contains([]byte(status), []byte(want)) {
			t.Errorf("J9 = %q, want it to contain %q", status, want)
		}
	}
}

func TestAttendanceWorkbookSummaryValues(t *testing.T) {
	f := roundTrip(t, "2026-08-01", "2026-08-31", testSchedule(), testRecords(), testSummaries())
	const sheet = "Сводка"

	checks := map[string]string{
		"A7":  "Иванов Иван",
		"C7":  "18",
		"D7":  "3",     // 21 expected − 18 present
		"E7":  "3",     // late days
		"F7":  "1:35",  // 95 minutes late in total
		"I7":  "09:12", // mean arrival, 552 minutes past midnight
		"K7":  "144:00",
		"M7":  "83%",
		"A9":  "Отсутствующий",
		"D9":  "21", // never came in: every workday counts as missed
		"I9":  "—",
		"M9":  "—",
		"A10": "ИТОГО",
		"C10": "39",
		"E10": "3",
	}
	for cell, want := range checks {
		got, _ := f.GetCellValue(sheet, cell)
		if got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
}

// Records must be stamped with the office wall clock whatever zone the
// database hands the timestamp back in — the same rule computeAttendanceStatus
// follows, and the bug class that made every arrival read five hours off.
func TestAttendanceWorkbookUsesLocalWallClock(t *testing.T) {
	utcPlus5 := time.FixedZone("UTC+5", 5*60*60)
	in := time.Date(2026, 8, 3, 9, 47, 0, 0, utcPlus5).UTC() // same instant, expressed as UTC
	rec := models.AttendanceView{
		AttendanceRecord: models.AttendanceRecord{
			WorkDate: "2026-08-03", CheckInAt: in,
			ExpectedStartMin: 540, ExpectedEndMin: 1080, GraceMinutes: 10, IsWorkday: true,
		},
		Username: "ivanov", DisplayName: "Иванов Иван",
	}

	orig := time.Local
	time.Local = utcPlus5
	defer func() { time.Local = orig }()

	f := roundTrip(t, "2026-08-01", "2026-08-31", testSchedule(), []models.AttendanceView{rec}, nil)
	if got, _ := f.GetCellValue("Детализация", "E7"); got != "09:47" {
		t.Errorf("check-in rendered as %q, want 09:47 — the UTC value would read 04:47", got)
	}
}

func TestAttendanceWorkbookEmptyRange(t *testing.T) {
	f := roundTrip(t, "2026-08-01", "2026-08-31", testSchedule(), nil, nil)
	got, _ := f.GetCellValue("Детализация", "A7")
	if got != "За выбранный период отметок нет" {
		t.Errorf("empty-range placeholder = %q", got)
	}
}
