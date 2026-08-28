package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"paylash/internal/db"
	"paylash/internal/models"
)

// Attendance report (XLSX). Deliberately Russian-only rather than following
// the caller's UI language: this workbook gets printed, emailed and filed by
// the studio's management, who work in Russian, and a document whose column
// headers changed depending on who happened to click Export would be worse
// than one that always reads the same.
//
// Every date/time cell is written as a real Excel serial number with a
// number format, never as pre-formatted text — so the recipient can sort by
// arrival time, filter by date and total a column, which is the whole point
// of handing them a spreadsheet instead of a CSV.

const (
	xlBrandFill  = "1F4E79" // header band
	xlBrandText  = "FFFFFF"
	xlTitleText  = "1F3864"
	xlMutedText  = "5A6470"
	xlGridLine   = "D6DCE4"
	xlZebraFill  = "F5F8FC"
	xlLateFill   = "FBE3E3"
	xlLateText   = "9C2323"
	xlEarlyFill  = "FFF1DC"
	xlReviewFill = "FFF6CC"
	xlOffFill    = "EDEFF2"
	xlGoodText   = "1E7A44"
	xlBadText    = "9C2323"
	xlWarnText   = "8A5200"
	xlTotalFill  = "E8EEF7"
)

// Excel number format codes. [h]:mm is elapsed time (can exceed 24h), as
// opposed to hh:mm which wraps — a month's worth of overtime must not
// silently render as "3:20" because it passed a day.
var (
	numFmtDate    = "dd.mm.yyyy"
	numFmtTime    = "hh:mm"
	numFmtElapsed = "[h]:mm"
	numFmtInt     = "0"
	numFmtPct     = `0"%"`
)

var ruWeekdaysLong = [7]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
var ruWeekdaysShort = [7]string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}

// excelDayStart is the epoch Excel counts serial days from (the 1900 date
// system, including its deliberate off-by-one leap-year bug, which is why
// this is Dec 30 and not Dec 31).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// excelSerial converts an instant to an Excel serial number, preserving the
// LOCAL wall clock: the same rule the rest of attendance follows (see
// computeAttendanceStatus). It rebuilds the local clock reading in UTC first
// so the arithmetic can never pick up an offset from the zone the timestamp
// arrived in.
func excelSerial(t time.Time) float64 {
	lt := t.In(time.Local)
	wall := time.Date(lt.Year(), lt.Month(), lt.Day(), lt.Hour(), lt.Minute(), lt.Second(), 0, time.UTC)
	return wall.Sub(excelEpoch).Hours() / 24
}

// excelTimeOfDay is the fraction-of-a-day part alone — what a cell formatted
// hh:mm needs when only the clock reading matters, not the date.
func excelTimeOfDay(t time.Time) float64 {
	lt := t.In(time.Local)
	return float64(lt.Hour()*3600+lt.Minute()*60+lt.Second()) / 86400
}

// excelMinutes converts a duration in minutes to the day fraction an
// elapsed-time ([h]:mm) cell expects.
func excelMinutes(min int) float64 { return float64(min) / 1440 }

func excelDateOnly(workDate string) (float64, bool) {
	d, err := time.ParseInLocation("2006-01-02", workDate, time.Local)
	if err != nil {
		return 0, false
	}
	return excelSerial(d), true
}

func ruDate(s string) string {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return d.Format("02.01.2006")
}

func minutesToHHMM(min int) string {
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// attnReportStyles holds every style id the two sheets use. Built once per
// workbook — excelize deduplicates, but resolving them up front keeps the
// row loops free of error handling for something that can't vary per row.
type attnReportStyles struct {
	title, subtitle, header                        int
	text, textAlt, textMuted                       int
	date, dateAlt, time_, timeAlt                  int
	elapsed, elapsedAlt, num, numAlt, pct, pctGood int
	pctWarn, pctBad                                int
	lateText, lateNum, offText                     int
	totalText, totalNum, totalElapsed, totalPct    int
	badgeLate, badgeEarly, badgeReview, badgeOff   int
}

type xlBuilder struct {
	f   *excelize.File
	err error
}

func (b *xlBuilder) set(sheet, cell string, v any) {
	if b.err == nil {
		b.err = b.f.SetCellValue(sheet, cell, v)
	}
}

func (b *xlBuilder) style(sheet, top, bottom string, id int) {
	if b.err == nil {
		b.err = b.f.SetCellStyle(sheet, top, bottom, id)
	}
}

func (b *xlBuilder) merge(sheet, top, bottom string) {
	if b.err == nil {
		b.err = b.f.MergeCell(sheet, top, bottom)
	}
}

func (b *xlBuilder) newStyle(s *excelize.Style) int {
	if b.err != nil {
		return 0
	}
	id, err := b.f.NewStyle(s)
	if err != nil {
		b.err = err
	}
	return id
}

func thinBorder() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: xlGridLine, Style: 1},
		{Type: "right", Color: xlGridLine, Style: 1},
		{Type: "top", Color: xlGridLine, Style: 1},
		{Type: "bottom", Color: xlGridLine, Style: 1},
	}
}

func fill(color string) excelize.Fill {
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}
}

func (b *xlBuilder) buildStyles() attnReportStyles {
	var s attnReportStyles
	body := func(numFmt *string, fillColor, textColor string, bold bool, horiz string) int {
		st := &excelize.Style{
			Border:    thinBorder(),
			Font:      &excelize.Font{Size: 10, Color: textColor, Bold: bold},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: horiz},
		}
		if numFmt != nil {
			st.CustomNumFmt = numFmt
		}
		if fillColor != "" {
			st.Fill = fill(fillColor)
		}
		return b.newStyle(st)
	}

	s.title = b.newStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 16, Bold: true, Color: xlTitleText},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	s.subtitle = b.newStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: xlMutedText},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	s.header = b.newStyle(&excelize.Style{
		Border:    thinBorder(),
		Fill:      fill(xlBrandFill),
		Font:      &excelize.Font{Size: 10, Bold: true, Color: xlBrandText},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	s.text = body(nil, "", "", false, "left")
	s.textAlt = body(nil, xlZebraFill, "", false, "left")
	s.textMuted = body(nil, "", xlMutedText, false, "left")
	s.date = body(&numFmtDate, "", "", false, "center")
	s.dateAlt = body(&numFmtDate, xlZebraFill, "", false, "center")
	s.time_ = body(&numFmtTime, "", "", false, "center")
	s.timeAlt = body(&numFmtTime, xlZebraFill, "", false, "center")
	s.elapsed = body(&numFmtElapsed, "", "", false, "center")
	s.elapsedAlt = body(&numFmtElapsed, xlZebraFill, "", false, "center")
	s.num = body(&numFmtInt, "", "", false, "center")
	s.numAlt = body(&numFmtInt, xlZebraFill, "", false, "center")
	s.pct = body(&numFmtPct, "", "", false, "center")
	s.pctGood = body(&numFmtPct, "", xlGoodText, true, "center")
	s.pctWarn = body(&numFmtPct, "", xlWarnText, true, "center")
	s.pctBad = body(&numFmtPct, "", xlBadText, true, "center")

	s.lateText = body(nil, xlLateFill, xlLateText, false, "left")
	s.lateNum = body(&numFmtInt, xlLateFill, xlLateText, true, "center")
	s.offText = b.newStyle(&excelize.Style{
		Border:    thinBorder(),
		Fill:      fill(xlOffFill),
		Font:      &excelize.Font{Size: 10, Color: xlMutedText, Italic: true},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left"},
	})

	s.badgeLate = body(nil, xlLateFill, xlLateText, true, "left")
	s.badgeEarly = body(nil, xlEarlyFill, xlWarnText, true, "left")
	s.badgeReview = body(nil, xlReviewFill, xlWarnText, true, "left")
	s.badgeOff = s.offText

	totalBorder := []excelize.Border{
		{Type: "top", Color: xlBrandFill, Style: 2},
		{Type: "bottom", Color: xlGridLine, Style: 1},
		{Type: "left", Color: xlGridLine, Style: 1},
		{Type: "right", Color: xlGridLine, Style: 1},
	}
	total := func(numFmt *string, horiz string) int {
		st := &excelize.Style{
			Border:    totalBorder,
			Fill:      fill(xlTotalFill),
			Font:      &excelize.Font{Size: 10, Bold: true, Color: xlTitleText},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: horiz},
		}
		if numFmt != nil {
			st.CustomNumFmt = numFmt
		}
		return b.newStyle(st)
	}
	s.totalText = total(nil, "left")
	s.totalNum = total(&numFmtInt, "center")
	s.totalElapsed = total(&numFmtElapsed, "center")
	s.totalPct = total(&numFmtPct, "center")
	return s
}

// reportHeaderRows writes the four-line masthead (title, period, schedule,
// generated-at) shared by both sheets and returns the row the table header
// goes on.
func (b *xlBuilder) reportHeaderRows(sheet, lastCol, from, to string, sched models.AttendanceSchedule, st attnReportStyles, extra string) int {
	workdays := make([]string, 0, len(sched.Workdays))
	for _, d := range sched.Workdays {
		if d >= 0 && d <= 6 {
			workdays = append(workdays, ruWeekdaysShort[d])
		}
	}
	expected := db.CountExpectedWorkdays(sched, from, to)

	b.merge(sheet, "A1", lastCol+"1")
	b.merge(sheet, "A2", lastCol+"2")
	b.merge(sheet, "A3", lastCol+"3")
	b.merge(sheet, "A4", lastCol+"4")
	b.set(sheet, "A1", "Отчёт по посещаемости")
	b.set(sheet, "A2", fmt.Sprintf("Период: %s — %s  ·  рабочих дней в периоде: %d%s", ruDate(from), ruDate(to), expected, extra))
	b.set(sheet, "A3", fmt.Sprintf("График: %s–%s  ·  допустимое опоздание: %d мин  ·  рабочие дни: %s",
		minutesToHHMM(sched.StartMin), minutesToHHMM(sched.EndMin), sched.GraceMinutes, strings.Join(workdays, ", ")))
	b.set(sheet, "A4", "Сформирован: "+time.Now().In(time.Local).Format("02.01.2006 15:04"))
	b.style(sheet, "A1", lastCol+"1", st.title)
	b.style(sheet, "A2", lastCol+"4", st.subtitle)
	if b.err == nil {
		b.err = b.f.SetRowHeight(sheet, 1, 28)
	}
	return 6
}

// freezeAndFilter pins the masthead + header row in place and turns the
// header into a filter bar, so a 500-row month stays navigable.
func (b *xlBuilder) freezeAndFilter(sheet, lastCol string, headerRow, lastRow int) {
	topLeft := fmt.Sprintf("A%d", headerRow+1)
	if b.err == nil {
		b.err = b.f.SetPanes(sheet, &excelize.Panes{
			Freeze: true, Split: false, XSplit: 0, YSplit: headerRow, TopLeftCell: topLeft,
			ActivePane: "bottomLeft",
			Selection:  []excelize.Selection{{SQRef: topLeft, ActiveCell: topLeft, Pane: "bottomLeft"}},
		})
	}
	if b.err == nil && lastRow > headerRow {
		b.err = b.f.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", headerRow, lastCol, lastRow), nil)
	}
	if b.err == nil {
		landscape, fitW, fitH := "landscape", 1, 0
		b.err = b.f.SetPageLayout(sheet, &excelize.PageLayoutOptions{
			Orientation: &landscape, FitToWidth: &fitW, FitToHeight: &fitH,
		})
	}
}

type xlColumn struct {
	title string
	width float64
}

func (b *xlBuilder) writeHeader(sheet string, row int, cols []xlColumn, st attnReportStyles) {
	for i, c := range cols {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			b.err = err
			return
		}
		b.set(sheet, fmt.Sprintf("%s%d", col, row), c.title)
		if b.err == nil {
			b.err = b.f.SetColWidth(sheet, col, col, c.width)
		}
	}
	last, _ := excelize.ColumnNumberToName(len(cols))
	b.style(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("%s%d", last, row), st.header)
	if b.err == nil {
		b.err = b.f.SetRowHeight(sheet, row, 30)
	}
}

// cellRef is a small convenience so the row loops read as (column index, row)
// rather than a pile of string concatenation.
func cellRef(colIdx, row int) string {
	name, _ := excelize.ColumnNumberToName(colIdx)
	return fmt.Sprintf("%s%d", name, row)
}

// attendanceStatusRU renders one record's verdict as the words a manager
// reading a printout expects, in the same priority order the UI badges use.
func attendanceStatusRU(r models.AttendanceView) string {
	var parts []string
	if !r.IsWorkday {
		parts = append(parts, "Выходной")
	}
	if r.IsLate {
		parts = append(parts, fmt.Sprintf("Опоздание %d мин", r.LateMinutes))
	}
	if r.IsEarlyLeave {
		parts = append(parts, fmt.Sprintf("Ранний уход %d мин", r.EarlyLeaveMinutes))
	}
	if r.CheckOutAt == nil {
		parts = append(parts, "Нет отметки об уходе")
	}
	if r.NeedsReview {
		parts = append(parts, "Требует проверки")
	}
	if len(parts) == 0 {
		return "Вовремя"
	}
	return strings.Join(parts, " · ")
}

func employeeName(fullName, username string) string {
	if strings.TrimSpace(fullName) != "" {
		return fullName
	}
	return username
}

// buildAttendanceWorkbook assembles the two-sheet report: "Сводка" (one row
// per tracked employee, which is what management actually reads) first, then
// "Детализация" (every individual check-in). The caller owns closing it.
func buildAttendanceWorkbook(from, to string, sched models.AttendanceSchedule,
	records []models.AttendanceView, summaries []models.AttendanceEmployeeSummary) (*excelize.File, error) {

	b := &xlBuilder{f: excelize.NewFile()}
	st := b.buildStyles()

	const summarySheet = "Сводка"
	const detailSheet = "Детализация"
	if err := b.f.SetSheetName("Sheet1", summarySheet); err != nil {
		return nil, err
	}
	if _, err := b.f.NewSheet(detailSheet); err != nil {
		return nil, err
	}

	b.writeSummarySheet(summarySheet, from, to, sched, summaries, st)
	b.writeDetailSheet(detailSheet, from, to, sched, records, st)

	if b.err == nil {
		b.err = b.f.SetDocProps(&excelize.DocProperties{
			Title:    fmt.Sprintf("Отчёт по посещаемости %s — %s", ruDate(from), ruDate(to)),
			Subject:  "Учёт рабочего времени",
			Creator:  "Paýlaş",
			Language: "ru-RU",
		})
	}
	b.f.SetActiveSheet(0)
	if b.err != nil {
		b.f.Close()
		return nil, b.err
	}
	return b.f, nil
}

func (b *xlBuilder) writeSummarySheet(sheet, from, to string, sched models.AttendanceSchedule,
	rows []models.AttendanceEmployeeSummary, st attnReportStyles) {

	cols := []xlColumn{
		{"Сотрудник", 30}, {"Логин", 16}, {"Дней\nприсутствия", 12}, {"Пропущено\nдней", 11},
		{"Опозданий", 11}, {"Суммарное\nопоздание", 12}, {"Ранних\nуходов", 10}, {"Суммарный\nранний уход", 13},
		{"Средний\nприход", 11}, {"Средний\nрабочий день", 13}, {"Всего\nотработано", 12},
		{"Требует\nпроверки", 11}, {"Пунктуальность", 15},
	}
	lastCol, _ := excelize.ColumnNumberToName(len(cols))
	headerRow := b.reportHeaderRows(sheet, lastCol, from, to, sched, st, fmt.Sprintf("  ·  сотрудников в учёте: %d", len(rows)))
	b.writeHeader(sheet, headerRow, cols, st)

	tot := models.AttendanceEmployeeSummary{}
	pctSum, pctCount := 0, 0
	row := headerRow
	for i, s := range rows {
		row = headerRow + 1 + i
		alt := i%2 == 1
		pick := func(base, altStyle int) int {
			if alt {
				return altStyle
			}
			return base
		}
		absent := s.ExpectedWorkdays - s.DaysPresent
		if absent < 0 {
			absent = 0
		}

		b.set(sheet, cellRef(1, row), employeeName(s.DisplayName, s.Username))
		b.style(sheet, cellRef(1, row), cellRef(1, row), pick(st.text, st.textAlt))
		b.set(sheet, cellRef(2, row), s.Username)
		b.style(sheet, cellRef(2, row), cellRef(2, row), pick(st.text, st.textAlt))

		b.set(sheet, cellRef(3, row), s.DaysPresent)
		b.set(sheet, cellRef(4, row), absent)
		b.set(sheet, cellRef(5, row), s.LateCount)
		b.set(sheet, cellRef(7, row), s.EarlyLeaveCount)
		b.set(sheet, cellRef(12, row), s.NeedsReviewCount)
		for _, c := range []int{3, 4, 5, 7, 12} {
			b.style(sheet, cellRef(c, row), cellRef(c, row), pick(st.num, st.numAlt))
		}
		// Absences are the number a manager scans this sheet for — give the
		// cell the same red the detail sheet uses for a late row so a
		// problem employee is visible without reading a single figure.
		if absent > 0 {
			b.style(sheet, cellRef(4, row), cellRef(4, row), st.lateNum)
		}
		if s.LateCount > 0 {
			b.style(sheet, cellRef(5, row), cellRef(5, row), st.lateNum)
		}

		b.set(sheet, cellRef(6, row), excelMinutes(s.TotalLateMinutes))
		b.set(sheet, cellRef(8, row), excelMinutes(s.TotalEarlyMinutes))
		b.set(sheet, cellRef(10, row), excelMinutes(s.AvgWorkedMinutes))
		b.set(sheet, cellRef(11, row), excelMinutes(s.TotalWorkedMinutes))
		for _, c := range []int{6, 8, 10, 11} {
			b.style(sheet, cellRef(c, row), cellRef(c, row), pick(st.elapsed, st.elapsedAlt))
		}

		// -1 is the server's "never showed up, nothing to average" sentinel
		// (see AttendanceEmployeeSummary) — a dash, never a misleading 0.
		if s.AvgCheckInMinutes >= 0 {
			b.set(sheet, cellRef(9, row), float64(s.AvgCheckInMinutes)/1440)
			b.style(sheet, cellRef(9, row), cellRef(9, row), pick(st.time_, st.timeAlt))
		} else {
			b.set(sheet, cellRef(9, row), "—")
			b.style(sheet, cellRef(9, row), cellRef(9, row), pick(st.text, st.textAlt))
		}
		if s.PunctualityPct >= 0 {
			b.set(sheet, cellRef(13, row), s.PunctualityPct)
			style := st.pctBad
			if s.PunctualityPct >= 90 {
				style = st.pctGood
			} else if s.PunctualityPct >= 70 {
				style = st.pctWarn
			}
			b.style(sheet, cellRef(13, row), cellRef(13, row), style)
			pctSum += s.PunctualityPct
			pctCount++
		} else {
			b.set(sheet, cellRef(13, row), "—")
			b.style(sheet, cellRef(13, row), cellRef(13, row), pick(st.text, st.textAlt))
		}

		tot.DaysPresent += s.DaysPresent
		tot.LateCount += s.LateCount
		tot.TotalLateMinutes += s.TotalLateMinutes
		tot.EarlyLeaveCount += s.EarlyLeaveCount
		tot.TotalEarlyMinutes += s.TotalEarlyMinutes
		tot.NeedsReviewCount += s.NeedsReviewCount
		tot.TotalWorkedMinutes += s.TotalWorkedMinutes
		tot.ExpectedWorkdays += s.ExpectedWorkdays
	}

	if len(rows) > 0 {
		trow := row + 1
		b.merge(sheet, cellRef(1, trow), cellRef(2, trow))
		b.set(sheet, cellRef(1, trow), "ИТОГО")
		b.style(sheet, cellRef(1, trow), cellRef(2, trow), st.totalText)
		absent := tot.ExpectedWorkdays - tot.DaysPresent
		if absent < 0 {
			absent = 0
		}
		b.set(sheet, cellRef(3, trow), tot.DaysPresent)
		b.set(sheet, cellRef(4, trow), absent)
		b.set(sheet, cellRef(5, trow), tot.LateCount)
		b.set(sheet, cellRef(7, trow), tot.EarlyLeaveCount)
		b.set(sheet, cellRef(12, trow), tot.NeedsReviewCount)
		for _, c := range []int{3, 4, 5, 7, 12} {
			b.style(sheet, cellRef(c, trow), cellRef(c, trow), st.totalNum)
		}
		b.set(sheet, cellRef(6, trow), excelMinutes(tot.TotalLateMinutes))
		b.set(sheet, cellRef(8, trow), excelMinutes(tot.TotalEarlyMinutes))
		b.set(sheet, cellRef(11, trow), excelMinutes(tot.TotalWorkedMinutes))
		for _, c := range []int{6, 8, 11} {
			b.style(sheet, cellRef(c, trow), cellRef(c, trow), st.totalElapsed)
		}
		b.style(sheet, cellRef(9, trow), cellRef(10, trow), st.totalText)
		// Mean of the per-employee percentages, not on-time days over all
		// days: this row answers "how punctual is the team", where one
		// person's terrible month shouldn't be diluted by a colleague who
		// happened to work more days.
		if pctCount > 0 {
			b.set(sheet, cellRef(13, trow), pctSum/pctCount)
		}
		b.style(sheet, cellRef(13, trow), cellRef(13, trow), st.totalPct)
	}
	b.freezeAndFilter(sheet, lastCol, headerRow, row)
}

func (b *xlBuilder) writeDetailSheet(sheet, from, to string, sched models.AttendanceSchedule,
	records []models.AttendanceView, st attnReportStyles) {

	cols := []xlColumn{
		{"Дата", 12}, {"День недели", 14}, {"Сотрудник", 30}, {"Логин", 16},
		{"Приход", 10}, {"Уход", 10}, {"Отработано", 12},
		{"Опоздание,\nмин", 12}, {"Ранний уход,\nмин", 13}, {"Статус", 34}, {"Примечание", 30},
	}
	lastCol, _ := excelize.ColumnNumberToName(len(cols))
	headerRow := b.reportHeaderRows(sheet, lastCol, from, to, sched, st, fmt.Sprintf("  ·  записей: %d", len(records)))
	b.writeHeader(sheet, headerRow, cols, st)

	row := headerRow
	for i, r := range records {
		row = headerRow + 1 + i
		alt := i%2 == 1

		// Row tint carries the worst thing about the day, in the same
		// priority order the UI badges use: unresolved review beats
		// lateness beats a plain day off.
		textStyle, numStyle, timeStyle, elapsedStyle, dateStyle := st.text, st.num, st.time_, st.elapsed, st.date
		if alt {
			textStyle, numStyle, timeStyle, elapsedStyle, dateStyle = st.textAlt, st.numAlt, st.timeAlt, st.elapsedAlt, st.dateAlt
		}
		rowTint := 0
		switch {
		case r.NeedsReview:
			rowTint = st.badgeReview
		case r.IsLate:
			rowTint = st.badgeLate
		case r.IsEarlyLeave:
			rowTint = st.badgeEarly
		case !r.IsWorkday:
			rowTint = st.badgeOff
		}

		if serial, ok := excelDateOnly(r.WorkDate); ok {
			b.set(sheet, cellRef(1, row), serial)
			b.style(sheet, cellRef(1, row), cellRef(1, row), dateStyle)
		} else {
			b.set(sheet, cellRef(1, row), r.WorkDate)
			b.style(sheet, cellRef(1, row), cellRef(1, row), textStyle)
		}

		weekday := ""
		if d, err := time.Parse("2006-01-02", r.WorkDate); err == nil {
			weekday = ruWeekdaysLong[int(d.Weekday())]
		}
		b.set(sheet, cellRef(2, row), weekday)
		b.set(sheet, cellRef(3, row), employeeName(r.DisplayName, r.Username))
		b.set(sheet, cellRef(4, row), r.Username)
		b.set(sheet, cellRef(11, row), r.Notes)
		for _, c := range []int{2, 3, 4, 11} {
			b.style(sheet, cellRef(c, row), cellRef(c, row), textStyle)
		}

		b.set(sheet, cellRef(5, row), excelTimeOfDay(r.CheckInAt))
		b.style(sheet, cellRef(5, row), cellRef(5, row), timeStyle)
		if r.CheckOutAt != nil {
			b.set(sheet, cellRef(6, row), excelTimeOfDay(*r.CheckOutAt))
			b.style(sheet, cellRef(6, row), cellRef(6, row), timeStyle)
			b.set(sheet, cellRef(7, row), excelMinutes(r.WorkedMinutes))
			b.style(sheet, cellRef(7, row), cellRef(7, row), elapsedStyle)
		} else {
			// A blank cell would be read as "0:00 worked"; an em dash says
			// the departure was never recorded, which is a different fact.
			b.set(sheet, cellRef(6, row), "—")
			b.set(sheet, cellRef(7, row), "—")
			b.style(sheet, cellRef(6, row), cellRef(7, row), textStyle)
		}

		if r.LateMinutes > 0 {
			b.set(sheet, cellRef(8, row), r.LateMinutes)
		}
		if r.EarlyLeaveMinutes > 0 {
			b.set(sheet, cellRef(9, row), r.EarlyLeaveMinutes)
		}
		b.style(sheet, cellRef(8, row), cellRef(9, row), numStyle)

		b.set(sheet, cellRef(10, row), attendanceStatusRU(r))
		if rowTint != 0 {
			b.style(sheet, cellRef(10, row), cellRef(10, row), rowTint)
			if r.LateMinutes > 0 {
				b.style(sheet, cellRef(8, row), cellRef(8, row), st.lateNum)
			}
		} else {
			b.style(sheet, cellRef(10, row), cellRef(10, row), textStyle)
		}
	}

	if len(records) == 0 {
		b.merge(sheet, cellRef(1, headerRow+1), cellRef(len(cols), headerRow+1))
		b.set(sheet, cellRef(1, headerRow+1), "За выбранный период отметок нет")
		b.style(sheet, cellRef(1, headerRow+1), cellRef(len(cols), headerRow+1), st.textMuted)
	}
	b.freezeAndFilter(sheet, lastCol, headerRow, row)
}
