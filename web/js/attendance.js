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

    async init() {
        try { this._today = await API.attendance.today(); } catch { this._today = null; }
        this.renderInto();
    },

    renderInto() {
        const el = document.getElementById('attendance-widget');
        if (el) el.innerHTML = this.html();
    },

    html() {
        const t = this._today;
        if (!t) {
            return `<button class="attn-widget-btn attn-widget-in" ${this._busy ? 'disabled' : ''} onclick="AttendanceWidget.checkIn()">
                🟢 ${I18N.t('attendance.check_in_button')}
            </button>`;
        }
        if (!t.check_out_at) {
            return `
            <div class="attn-widget-status">${I18N.t('attendance.checked_in_at', { time: UI.formatTime(t.check_in_at) })}</div>
            <button class="attn-widget-btn attn-widget-out" ${this._busy ? 'disabled' : ''} onclick="AttendanceWidget.checkOut()">
                🔴 ${I18N.t('attendance.check_out_button')}
            </button>`;
        }
        return `<div class="attn-widget-status attn-widget-done">${I18N.t('attendance.day_done', { in: UI.formatTime(t.check_in_at), out: UI.formatTime(t.check_out_at) })}</div>`;
    },

    async checkIn() {
        if (this._busy) return;
        this._busy = true; this.renderInto();
        try {
            this._today = await API.attendance.checkIn();
            UI.toast(I18N.t('attendance.check_in_toast', { time: UI.formatTime(this._today.check_in_at) }), 'success');
            if (this._today.is_late) UI.toast(I18N.tn('attendance.late_by', this._today.late_minutes), 'info');
            AttendancePage.refreshIfOpen();
        } catch (e) { UI.toast(e.message, 'error'); }
        finally { this._busy = false; this.renderInto(); }
    },

    async checkOut() {
        if (this._busy) return;
        this._busy = true; this.renderInto();
        try {
            this._today = await API.attendance.checkOut();
            UI.toast(I18N.t('attendance.check_out_toast', { time: UI.formatTime(this._today.check_out_at) }), 'success');
            if (this._today.is_early_leave) UI.toast(I18N.tn('attendance.early_by', this._today.early_leave_minutes), 'info');
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

    todayCardHTML(t) {
        if (!t) {
            return `<div class="attn-today-inner">
                <div class="attn-today-clock" id="attn-live-clock"></div>
                <button class="btn btn-primary btn-lg" onclick="AttendanceWidget.checkIn().then(()=>AttendancePage.load())">${I18N.t('attendance.check_in_button')}</button>
            </div>`;
        }
        if (!t.check_out_at) {
            return `<div class="attn-today-inner">
                <div class="attn-today-line">${I18N.t('attendance.checked_in_at', { time: UI.formatTime(t.check_in_at) })}</div>
                <button class="btn btn-danger btn-lg" onclick="AttendanceWidget.checkOut().then(()=>AttendancePage.load())">${I18N.t('attendance.check_out_button')}</button>
            </div>`;
        }
        return `<div class="attn-today-inner">
            <div class="attn-today-line attn-today-done">${I18N.t('attendance.day_done', { in: UI.formatTime(t.check_in_at), out: UI.formatTime(t.check_out_at) })}</div>
            <div class="attn-today-worked">${I18N.t('attendance.worked_label')}: ${UI.formatAttnDuration(t.worked_minutes)}</div>
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
