/* Paylash — Attendance (check-in/check-out + personal history).
   Two pieces share this file: AttendanceWidget (compact, rendered inside
   the sidebar on every page — see App.renderShell) and AttendancePage (the
   full personal history view at /attendance). Both call the same
   API.attendance.* endpoints; the recorded time always comes back from the
   server's response (internal/api/attendance.go stamps time.Now() itself),
   never from anything the client sent. */
const AttendanceWidget = {
    _today: null,
    _busy: false,

    // An account an admin has taken off the clock (users.attendance_tracked)
    // gets no widget at all — a check-in button the server would refuse is
    // worse than no button. The server enforces it too; this is presentation.
    tracked() { return !App.user || App.user.attendance_tracked !== false; },

    async init() {
        if (!this.tracked()) { this._today = null; this.renderInto(); return; }
        try { this._today = await API.attendance.today(); } catch { this._today = null; }
        this.renderInto();
    },

    renderInto() {
        const el = document.getElementById('attendance-widget');
        if (el) el.innerHTML = this.tracked() ? this.html() : '';
    },

    html() {
        const t = this._today;
        let status = '';
        if (t && t.check_out_at) {
            status = `<div class="attn-widget-status attn-widget-done">${I18N.t('attendance.day_done', { in: UI.formatTime(t.check_in_at), out: UI.formatTime(t.check_out_at) })}</div>`;
        } else if (t) {
            status = `<div class="attn-widget-status">${I18N.t('attendance.checked_in_at', { time: UI.formatTime(t.check_in_at) })}</div>`;
        }
        return status + this.splitHTML();
    },

    /* ── The check-in / check-out control ────────────────────── */

    // Arrival on the left, departure on the right, in the order the day
    // happens — and both halves are always present, with exactly one of them
    // ever actionable.
    //
    // Rendered here rather than in each caller so the sidebar widget and the
    // Attendance page cannot disagree about which half is live: they used to
    // decide that independently, which is two copies of one rule.
    //
    // Deliberately DISABLED rather than hidden. This is a control people
    // press twice a day without looking, so the button under the cursor has
    // to stay where it was all day; one that changes shape between states
    // invites pressing the wrong thing.
    splitHTML(extraClass) {
        const t = this._today;
        const checkedIn = !!t;
        const done = !!(t && t.check_out_at);
        const inOff = this._busy || checkedIn;
        const outOff = this._busy || !checkedIn || done;

        // The visible label is the short form — half a 240px sidebar cannot
        // hold "Отметить приход", and both halves truncating to the same
        // "Отметить ..." is worse than terse. The full wording lives in the
        // title, which also carries the reason when a half is disabled:
        // otherwise "why can I not press that" has no answer on screen.
        const inTitle = checkedIn ? I18N.t('attendance.already_checked_in') : I18N.t('attendance.check_in_button');
        const outTitle = done ? I18N.t('attendance.already_checked_out')
            : !checkedIn ? I18N.t('attendance.check_in_first')
                : I18N.t('attendance.check_out_button');

        return `<div class="attn-split ${extraClass || ''}" role="group" aria-label="${I18N.t('attendance.widget_label')}">
            <button type="button" class="attn-split-btn attn-split-in" ${inOff ? 'disabled' : ''}
                    title="${inTitle}" onclick="AttendanceWidget.checkIn()">
                <span class="attn-split-dot">🟢</span><span class="attn-split-text">${I18N.t('attendance.check_in_short')}</span>
            </button>
            <button type="button" class="attn-split-btn attn-split-out" ${outOff ? 'disabled' : ''}
                    title="${outTitle}" onclick="AttendanceWidget.checkOut()">
                <span class="attn-split-dot">🔴</span><span class="attn-split-text">${I18N.t('attendance.check_out_short')}</span>
            </button>
        </div>`;
    },

    // How many minutes early a check-out RIGHT NOW would be, or 0.
    //
    // Mirrors computeAttendanceStatus in internal/db/attendance.go exactly:
    // the expected end is measured from midnight of the CHECK-IN's date, a
    // non-workday is never flagged, and — unlike arriving — leaving has no
    // grace period, so 17:59 against an 18:00 day really is early.
    //
    // This reads the browser's clock while the verdict belongs to the
    // server, so a machine with a wrong clock or zone can disagree. That is
    // acceptable for something that only asks a question: what gets recorded
    // is still computed server-side from its own time.Now(). It fails open —
    // anything missing or unparseable yields 0 and the check-out proceeds
    // with no dialog, because a UI detail must never be the thing that stops
    // someone going home.
    earlyLeaveMinutes() {
        const t = this._today;
        if (!t || t.check_out_at || t.is_workday === false) return 0;
        if (typeof t.expected_end_min !== 'number') return 0;
        const inAt = new Date(t.check_in_at);
        if (isNaN(inAt.getTime())) return 0;
        const end = new Date(inAt.getFullYear(), inAt.getMonth(), inAt.getDate(), 0, 0, 0, 0);
        end.setMinutes(end.getMinutes() + t.expected_end_min);
        const left = Math.round((end.getTime() - Date.now()) / 60000);
        return left > 0 ? left : 0;
    },

    async checkIn() {
        if (this._busy) return;
        this._busy = true; this.renderInto();
        try {
            this._today = await API.attendance.checkIn();
            UI.toast(I18N.t('attendance.check_in_toast', { time: UI.formatTime(this._today.check_in_at) }), 'success');
            if (this._today.is_late) UI.toast(I18N.t('attendance.late_by', { duration: UI.formatAttnDurationPhrase(this._today.late_minutes) }), 'info');
            AttendancePage.refreshIfOpen();
        } catch (e) { UI.toast(e.message, 'error'); }
        finally { this._busy = false; this.renderInto(); }
    },

    // Asks before recording a departure the server will mark as early. The
    // question names the fact that makes it worth asking — when the day ends
    // and how much of it is left — rather than a bare "are you sure", which
    // tells someone nothing they did not already know.
    async checkOut() {
        if (this._busy) return;
        const early = this.earlyLeaveMinutes();
        if (early > 0) {
            UI.confirmAction(
                I18N.t('attendance.early_confirm_title'),
                I18N.t('attendance.early_confirm_body', {
                    end: UI.formatMinutesAsTime(this._today.expected_end_min),
                    left: UI.formatAttnDuration(early),
                }),
                I18N.t('attendance.early_confirm_yes'),
                () => this._doCheckOut(),
            );
            return;
        }
        await this._doCheckOut();
    },

    async _doCheckOut() {
        if (this._busy) return;
        this._busy = true; this.renderInto();
        try {
            this._today = await API.attendance.checkOut();
            UI.toast(I18N.t('attendance.check_out_toast', { time: UI.formatTime(this._today.check_out_at) }), 'success');
            if (this._today.is_early_leave) UI.toast(I18N.t('attendance.early_by', { duration: UI.formatAttnDurationPhrase(this._today.early_leave_minutes) }), 'info');
            AttendancePage.refreshIfOpen();
        } catch (e) { UI.toast(e.message, 'error'); }
        finally { this._busy = false; this.renderInto(); }
    },
};

const AttendancePage = {
    _history: [],

    render() {
        return `
        <div class="attn-page">
            <div class="attn-today-card" id="attn-today-card">${UI.skeletonCards(1)}</div>
            <div class="stat-cards" id="attn-summary"></div>
            <h3 style="font-size:1rem;font-weight:600;margin:20px 0 12px">${I18N.t('attendance.history_title')}</h3>
            <div id="attn-history">${UI.skeletonCards(4)}</div>
        </div>`;
    },

    async init() { await this.load(); },

    refreshIfOpen() { if (App.currentPage === 'attendance') this.load(); },

    async load() {
        const todayCard = document.getElementById('attn-today-card');
        const histEl = document.getElementById('attn-history');
        // Untracked: past records (if any) still belong to the person and
        // stay readable — only the check-in controls go away.
        if (!AttendanceWidget.tracked()) {
            this.stopClock();
            if (todayCard) todayCard.innerHTML = `<div class="attn-today-inner"><div class="attn-today-line attn-not-tracked">${I18N.t('attendance.not_tracked')}</div><div class="attn-today-worked">${I18N.t('attendance.not_tracked_hint')}</div></div>`;
            try {
                this._history = await API.attendance.myHistory(UI.dateDaysAgo(30)) || [];
                this.renderSummary();
                if (histEl) histEl.innerHTML = this.historyHTML();
            } catch (e) {
                if (histEl) histEl.innerHTML = `<div class="empty-state"><p>${UI.esc(e.message)}</p></div>`;
            }
            return;
        }
        try {
            const [today, history] = await Promise.all([
                API.attendance.today(),
                API.attendance.myHistory(UI.dateDaysAgo(30)),
            ]);
            AttendanceWidget._today = today;
            this._history = history || [];
            if (todayCard) todayCard.innerHTML = this.todayCardHTML(today);
            this.renderSummary();
            if (histEl) histEl.innerHTML = this.historyHTML();
            if (!today) this.startClock(); else this.stopClock();
        } catch (e) {
            if (histEl) histEl.innerHTML = `<div class="empty-state"><p>${UI.esc(e.message)}</p></div>`;
        }
    },

    // Purely decorative — the actual recorded time always comes back from
    // the server's response (see AttendanceWidget.checkIn/checkOut), never
    // from this. Self-stops the moment the element or the page is gone, so
    // navigating away never leaves a dangling interval running.
    _clockTimer: null,
    startClock() {
        this.stopClock();
        const tick = () => {
            const el = document.getElementById('attn-live-clock');
            if (!el || App.currentPage !== 'attendance') { this.stopClock(); return; }
            el.textContent = new Date().toLocaleTimeString(I18N.dateLocale(), { hour: '2-digit', minute: '2-digit', second: '2-digit' });
        };
        tick();
        this._clockTimer = setInterval(tick, 1000);
    },
    stopClock() { if (this._clockTimer) { clearInterval(this._clockTimer); this._clockTimer = null; } },

    // Uses AttendanceWidget.splitHTML for the buttons, so this card and the
    // sidebar can never disagree about which half is live.
    //
    // They no longer chain .then(() => this.load()): checkIn/checkOut already
    // call refreshIfOpen(), so that was a second, redundant fetch — and it
    // would now be outright wrong, since an early check-out returns
    // immediately to put a confirmation on screen and finishes later.
    todayCardHTML(t) {
        if (!t) {
            return `<div class="attn-today-inner">
                <div class="attn-today-clock" id="attn-live-clock"></div>
                ${AttendanceWidget.splitHTML('attn-split-lg')}
            </div>`;
        }
        if (!t.check_out_at) {
            return `<div class="attn-today-inner">
                <div class="attn-today-line">${I18N.t('attendance.checked_in_at', { time: UI.formatTime(t.check_in_at) })}</div>
                ${AttendanceWidget.splitHTML('attn-split-lg')}
            </div>`;
        }
        return `<div class="attn-today-inner">
            <div class="attn-today-line attn-today-done">${I18N.t('attendance.day_done', { in: UI.formatTime(t.check_in_at), out: UI.formatTime(t.check_out_at) })}</div>
            <div class="attn-today-worked">${I18N.t('attendance.worked_label')}: ${UI.formatAttnDuration(t.worked_minutes)}</div>
            ${AttendanceWidget.splitHTML('attn-split-lg')}
        </div>`;
    },

    renderSummary() {
        const el = document.getElementById('attn-summary');
        if (!el) return;
        const days = this._history.length;
        const late = this._history.filter(r => r.is_late).length;
        const early = this._history.filter(r => r.is_early_leave).length;
        el.innerHTML = `
            <div class="stat-card"><div class="stat-card-value">${days}</div><div class="stat-card-label">${I18N.t('attendance.days_worked')}</div></div>
            <div class="stat-card"><div class="stat-card-value">${late}</div><div class="stat-card-label">${I18N.t('attendance.late_count')}</div></div>
            <div class="stat-card"><div class="stat-card-value">${early}</div><div class="stat-card-label">${I18N.t('attendance.early_count')}</div></div>`;
    },

    historyHTML() {
        if (!this._history.length) return `<div class="empty-state"><p class="text-muted">${I18N.t('attendance.no_history')}</p></div>`;
        return `<div class="table-responsive"><table class="admin-table">
            <thead><tr><th>${I18N.t('attendance.col_date')}</th><th>${I18N.t('attendance.col_check_in')}</th><th>${I18N.t('attendance.col_check_out')}</th><th>${I18N.t('attendance.col_worked')}</th><th>${I18N.t('attendance.col_status')}</th></tr></thead>
            <tbody>${this._history.map(r => `<tr>
                <td>${UI.esc(r.work_date)}</td>
                <td>${UI.formatTime(r.check_in_at)}</td>
                <td>${r.check_out_at ? UI.formatTime(r.check_out_at) : '—'}</td>
                <td>${r.check_out_at ? UI.formatAttnDuration(r.worked_minutes) : '—'}</td>
                <td>${UI.attendanceStatusBadges(r)}</td>
            </tr>`).join('')}</tbody>
        </table></div>`;
    },
};
