package api

import (
	"encoding/csv"
	"log"
	"net/http"
	"strconv"
	"time"

	"paylash/internal/authutil"
	"paylash/internal/models"
)

// CheckIn records the caller's arrival for today. Time comes ONLY from
// time.Now() on the server (see db.CheckIn) — the request carries no
// timestamp, so a manipulated client clock can never affect what gets
// recorded.
func (h *Handler) CheckIn(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	sched, err := h.db.GetAttendanceSchedule()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	rec, err := h.db.CheckIn(user.ID, sched)
	if err != nil {
		writeError(w, http.StatusConflict, "eýýäm gelendigiňizi bellediňiz")
		return
	}
	h.logAction(r, "attendance.check_in", "attendance", rec.ID, "", nil)
	writeJSON(w, http.StatusCreated, rec)
}

// CheckOut records the caller's departure for today — same server-time-only
// rule as CheckIn.
func (h *Handler) CheckOut(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	rec, err := h.db.CheckOut(user.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "ilki gelendigiňizi bellemeli")
		return
	}
	h.logAction(r, "attendance.check_out", "attendance", rec.ID, "", nil)
	writeJSON(w, http.StatusOK, rec)
}

// MyTodayAttendance returns the caller's own record for today, or `null` if
// they haven't checked in yet — backs the check-in/check-out widget's
// initial state on page load.
func (h *Handler) MyTodayAttendance(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	rec, err := h.db.GetTodayAttendance(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// MyAttendanceHistory returns the caller's own records — backs the personal
// attendance page. Nobody, including admin, can read another user's history
// through this endpoint; that's what AdminListAttendance is for.
func (h *Handler) MyAttendanceHistory(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	from, to, err := parseAttendanceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry senä aralygy")
		return
	}
	list, err := h.db.ListMyAttendance(user.ID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AdminListAttendance returns every employee's records for the admin/manager
// "Посещаемость" table — read-only, so ManagerOrAdminMiddleware (not
// AdminMiddleware) gates this route.
func (h *Handler) AdminListAttendance(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseAttendanceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry senä aralygy")
		return
	}
	var userID *int
	if v := r.URL.Query().Get("user_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "nädogry ulanyjy")
			return
		}
		userID = &id
	}
	list, err := h.db.ListAttendance(from, to, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AdminAttendanceAnalytics backs the stat cards + trend chart at the top of
// the admin/manager attendance tab.
func (h *Handler) AdminAttendanceAnalytics(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseAttendanceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry senä aralygy")
		return
	}
	a, err := h.db.GetAttendanceAnalytics(from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// AdminAttendanceSummary returns one aggregated row per employee for the
// range — backs the per-employee monthly analytics table. Read-only, so
// ManagerOrAdminMiddleware gates it like the other listing endpoints.
func (h *Handler) AdminAttendanceSummary(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseAttendanceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry senä aralygy")
		return
	}
	list, err := h.db.GetAttendanceEmployeeSummaries(from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AdminExportAttendance streams the same range AdminListAttendance would
// return as CSV — same streaming-writer pattern as AdminExportAuditLog.
func (h *Handler) AdminExportAttendance(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseAttendanceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry senä aralygy")
		return
	}
	list, err := h.db.ListAttendance(from, to, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="paylash-attendance.csv"`)
	cw := csv.NewWriter(w)
	cw.Write([]string{"date", "employee", "username", "check_in", "check_out", "late_minutes", "early_leave_minutes", "worked_minutes", "needs_review", "notes"})
	for _, v := range list {
		checkOut := ""
		if v.CheckOutAt != nil {
			checkOut = v.CheckOutAt.Format(time.RFC3339)
		}
		cw.Write([]string{
			v.WorkDate, v.DisplayName, v.Username, v.CheckInAt.Format(time.RFC3339), checkOut,
			strconv.Itoa(v.LateMinutes), strconv.Itoa(v.EarlyLeaveMinutes), strconv.Itoa(v.WorkedMinutes),
			strconv.FormatBool(v.NeedsReview), v.Notes,
		})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		log.Printf("export attendance: %v", err)
	}
}

// AdminUpdateAttendanceRecord is the admin-only correction path (fixing a
// mistaken time, or resolving a needs_review row after checking with the
// employee) — AdminMiddleware, never ManagerOrAdminMiddleware: a manager can
// see attendance but never edit it.
func (h *Handler) AdminUpdateAttendanceRecord(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	var req struct {
		CheckInAt  time.Time  `json:"check_in_at"`
		CheckOutAt *time.Time `json:"check_out_at"`
		Notes      string     `json:"notes"`
	}
	if err := readJSON(r, &req); err != nil || req.CheckInAt.IsZero() {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.CheckOutAt != nil && req.CheckOutAt.Before(req.CheckInAt) {
		writeError(w, http.StatusBadRequest, "gitme wagty gelme wagtyndan öň bolup bilmez")
		return
	}
	if err := h.db.UpdateAttendanceRecord(id, req.CheckInAt, req.CheckOutAt, req.Notes); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	rec, err := h.db.GetAttendanceRecordByID(id)
	if err != nil || rec == nil {
		writeError(w, http.StatusNotFound, "ýazgy tapylmady")
		return
	}
	h.logAction(r, "attendance.correct", "attendance", id, "", map[string]any{"user_id": rec.UserID})
	writeJSON(w, http.StatusOK, rec)
}

// AdminGetAttendanceSchedule/AdminSetAttendanceSchedule configure the single
// company-wide schedule every check-in/check-out is measured against. Get is
// ManagerOrAdminMiddleware (a manager can see what "late" means); Set is
// AdminMiddleware only.
func (h *Handler) AdminGetAttendanceSchedule(w http.ResponseWriter, r *http.Request) {
	sched, err := h.db.GetAttendanceSchedule()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, sched)
}

func (h *Handler) AdminSetAttendanceSchedule(w http.ResponseWriter, r *http.Request) {
	var req models.AttendanceSchedule
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.StartMin < 0 || req.StartMin > 1439 || req.EndMin < 0 || req.EndMin > 1439 || req.StartMin >= req.EndMin {
		writeError(w, http.StatusBadRequest, "nädogry iş wagty")
		return
	}
	if req.GraceMinutes < 0 || req.GraceMinutes > 120 {
		writeError(w, http.StatusBadRequest, "nädogry rugsat wagty")
		return
	}
	for _, wd := range req.Workdays {
		if wd < 0 || wd > 6 {
			writeError(w, http.StatusBadRequest, "nädogry iş güni")
			return
		}
	}
	if err := h.db.SetAttendanceSchedule(req); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	h.logAction(r, "attendance.schedule_update", "attendance", 0, "", map[string]any{
		"start_min": req.StartMin, "end_min": req.EndMin, "grace_minutes": req.GraceMinutes, "workdays": req.Workdays,
	})
	writeJSON(w, http.StatusOK, req)
}

// parseAttendanceRange reads ?from=&to= (YYYY-MM-DD), defaulting to the
// last 30 days when either is missing — used by every listing/analytics/
// export endpoint above so they all share one validation path.
func parseAttendanceRange(r *http.Request) (from, to string, err error) {
	from = r.URL.Query().Get("from")
	to = r.URL.Query().Get("to")
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	if from == "" {
		from = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	if _, err = time.Parse("2006-01-02", from); err != nil {
		return "", "", err
	}
	if _, err = time.Parse("2006-01-02", to); err != nil {
		return "", "", err
	}
	return from, to, nil
}
