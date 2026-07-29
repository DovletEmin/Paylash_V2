/* Paylash — Admin Panel */
const AdminPage = {
    currentTab: 'dashboard',
    _users: [],
    _projects: [],

    render() {
        return `
        <div class="admin-page">
            <div class="admin-sidebar">
                <div class="admin-title">${UI.icons.settings} ${I18N.t('app.nav_admin_section')}</div>
                <nav class="admin-nav">
                    <a class="admin-nav-item ${this.currentTab === 'dashboard' ? 'active' : ''}" onclick="AdminPage.switchTab('dashboard')">${UI.icons.dashboard} ${I18N.t('admin.nav_dashboard')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'projects' ? 'active' : ''}" onclick="AdminPage.switchTab('projects')">${UI.icons.users} ${I18N.t('admin.nav_projects')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'users' ? 'active' : ''}" onclick="AdminPage.switchTab('users')">${UI.icons.user} ${I18N.t('admin.nav_users')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'audit-log' ? 'active' : ''}" onclick="AdminPage.switchTab('audit-log')">🕓 ${I18N.t('admin.nav_audit_log')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'uploads' ? 'active' : ''}" onclick="AdminPage.switchTab('uploads')">⬆ ${I18N.t('admin.nav_uploads')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'chat-reports' ? 'active' : ''}" onclick="AdminPage.switchTab('chat-reports')">🚩 ${I18N.t('admin.nav_chat_reports')}${this._openReportCount ? ` <span class="nav-badge">${this._openReportCount}</span>` : ''}</a>
                </nav>
            </div>
            <div class="admin-content" id="admin-content"></div>
        </div>`;
    },

    async init() {
        await this.switchTab(this.currentTab, true);
        this.refreshOpenReportCount();
    },

    // Restores the active tab from ?tab= — called by App.initPage before
    // render()/init() so refreshing on (say) the audit log doesn't silently
    // drop back to the dashboard.
    applyURLParams(params) {
        const tab = params && params.get('tab');
        const valid = ['dashboard', 'projects', 'users', 'audit-log', 'uploads', 'chat-reports'];
        if (tab && valid.includes(tab)) this.currentTab = tab;
    },

    // Kept separate from switchTab so the sidebar badge stays current even
    // while a different tab is open — polled the same lightweight way the
    // chat unread badge is (see App.checkChatUnread).
    async refreshOpenReportCount() {
        try {
            const res = await API.admin.chatReports.openCount();
            this._openReportCount = res.count || 0;
            const badge = document.querySelector('.admin-nav-item[onclick*="chat-reports"]');
            if (badge) badge.outerHTML = `<a class="admin-nav-item ${this.currentTab === 'chat-reports' ? 'active' : ''}" onclick="AdminPage.switchTab('chat-reports')">🚩 ${I18N.t('admin.nav_chat_reports')}${this._openReportCount ? ` <span class="nav-badge">${this._openReportCount}</span>` : ''}</a>`;
        } catch { /* best-effort — a stale/missing badge count isn't worth surfacing an error for */ }
    },

    // fromURLRestore is true only for the call inside init() itself —
    // avoids pushing a redundant history entry for a tab that's already
    // exactly what the current URL says (restoring from ?tab= or a plain
    // default). Every other call (clicking a sidebar tab, an action
    // re-rendering the tab it's already on) syncs the URL normally.
    async switchTab(tab, fromURLRestore) {
        this.currentTab = tab;
        if (!fromURLRestore && tab !== 'dashboard') App.updatePageURL({ tab }, false);
        else if (!fromURLRestore) App.updatePageURL({}, false);
        document.querySelectorAll('.admin-nav-item').forEach((el, i) => {
            el.classList.toggle('active', ['dashboard','projects','users','audit-log','uploads','chat-reports'][i] === tab);
        });
        const c = document.getElementById('admin-content');
        if (!c) return;
        c.innerHTML = '<div class="admin-loading"><div class="spinner"></div></div>';
        switch (tab) {
            case 'dashboard':    await this.renderDashboard(c); break;
            case 'projects':     await this.renderProjects(c); break;
            case 'users':        await this.renderUsers(c); break;
            case 'audit-log':    await this.renderAuditLog(c); break;
            case 'uploads':      await this.renderUploads(c); break;
            case 'chat-reports': await this.renderChatReports(c); break;
        }
    },

    /* ── Dashboard ── */
    async renderDashboard(el) {
        try {
            const [d, pq, trend] = await Promise.all([
                API.admin.dashboard(), API.admin.publicQuota.get(),
                API.admin.storageTrend(30).catch(() => []),
            ]);
            const pqGB = Math.round((pq.quota_bytes || 53687091200) / (1024 ** 3) * 10) / 10;
            el.innerHTML = `
            <h2 style="font-size:1.1rem;font-weight:600;margin-bottom:16px">${I18N.t('admin.nav_dashboard')}</h2>
            <div class="stat-cards">
                <div class="stat-card"><div class="stat-card-value">${d.total_users || 0}</div><div class="stat-card-label">${I18N.t('admin.nav_users')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_projects || 0}</div><div class="stat-card-label">${I18N.t('admin.nav_projects')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_files || 0}</div><div class="stat-card-label">${I18N.t('app.nav_files')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${UI.formatBytes(d.total_bytes || 0)}</div><div class="stat-card-label">${I18N.t('admin.stat_used_space')}</div></div>
            </div>
            <h3 style="font-size:1rem;font-weight:600;margin:24px 0 12px">${I18N.t('admin.trend_title')}</h3>
            ${this.renderTrendChart(trend)}
            <h3 style="font-size:1rem;font-weight:600;margin:24px 0 12px">${I18N.t('admin.public_quota_title')}</h3>
            <div style="display:flex;align-items:center;gap:10px">
                <input type="number" id="public-quota-gb" class="form-control" value="${pqGB}" min="0.1" step="0.1" style="width:160px">
                <span class="text-muted" style="font-size:.82rem">${I18N.t('admin.unit_gb')}</span>
                <button class="btn btn-primary btn-sm" onclick="AdminPage.savePublicQuota()">${I18N.t('common.save')}</button>
            </div>
            <p class="text-muted" style="font-size:.78rem;margin-top:6px">${I18N.t('admin.public_quota_hint')}</p>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    async savePublicQuota() {
        const gb = parseFloat(document.getElementById('public-quota-gb').value) || 0;
        if (gb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        try { await API.admin.publicQuota.set(Math.round(gb * 1024)); UI.toast(I18N.t('admin.public_quota_changed'), 'success'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    // Self-contained inline SVG line chart — no charting library, matching
    // this app's no-build-step/no-new-dependency approach everywhere else.
    // viewBox + preserveAspectRatio="none" lets it stretch to fill its
    // container's actual width via plain CSS, no resize-handling JS needed.
    renderTrendChart(points) {
        if (!points || points.length < 2) {
            return `<p class="text-muted" style="font-size:.82rem">${I18N.t('admin.trend_no_data')}</p>`;
        }
        const w = 600, h = 160, padL = 4, padR = 4, padT = 8, padB = 8;
        const innerW = w - padL - padR, innerH = h - padT - padB;
        const maxBytes = Math.max(...points.map(p => p.total_bytes), 1);
        const stepX = innerW / (points.length - 1);
        const coords = points.map((p, i) => {
            const x = padL + i * stepX;
            const y = padT + innerH - (p.total_bytes / maxBytes) * innerH;
            return `${x.toFixed(1)},${y.toFixed(1)}`;
        });
        const line = coords.join(' ');
        const area = `${padL},${padT + innerH} ${line} ${padL + innerW},${padT + innerH}`;
        const first = points[0], last = points[points.length - 1];
        return `<div class="trend-chart-wrap">
            <svg viewBox="0 0 ${w} ${h}" class="trend-chart" preserveAspectRatio="none" role="img" aria-label="${I18N.t('admin.trend_title')}">
                <polyline points="${area}" class="trend-chart-area"></polyline>
                <polyline points="${line}" class="trend-chart-line"></polyline>
            </svg>
            <div class="trend-chart-labels">
                <span>${UI.esc(first.date)} · ${UI.formatBytes(first.total_bytes)}</span>
                <span class="trend-chart-now">${I18N.t('admin.trend_now')}: ${UI.formatBytes(last.total_bytes)} (${last.file_count} ${I18N.t('app.nav_files').toLowerCase()})</span>
                <span>${UI.esc(last.date)}</span>
            </div>
        </div>`;
    },

    /* ── Projects ── */
    async renderProjects(el) {
        try {
            const items = (await API.admin.projects.list()) || [];
            this._projects = items;
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_projects')}</h2><div style="display:flex;gap:8px">
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.showBulkProjectQuota()">📊 ${I18N.t('admin.bulk_quota_all')}</button>
                <button class="btn btn-primary btn-sm" onclick="AdminPage.showProjectModal()">${UI.icons.plus} ${I18N.t('admin.new_project')}</button>
            </div></div>
            <p class="text-muted" style="font-size:.82rem;margin-bottom:12px">${I18N.t('admin.projects_hint')}</p>
            <table class="admin-table"><thead><tr><th>${I18N.t('admin.col_id')}</th><th>${I18N.t('admin.col_name')}</th><th>${I18N.t('admin.col_quota')}</th><th>${I18N.t('admin.col_actions')}</th></tr></thead><tbody>
            ${items.map(p => `<tr><td>${p.id}</td><td>${UI.esc(p.name)}</td><td>${UI.formatBytes(p.quota_bytes || 0)}</td><td>
                <button class="btn btn-sm btn-ghost" onclick="AdminPage.showMembersModal(${p.id},${UI.escJson(p.name)})">👥 ${I18N.t('admin.members_button')}</button>
                <button class="btn btn-sm btn-ghost" onclick="AdminPage.showProjectModal(${p.id},${UI.escJson(p.name)},${p.quota_bytes||0})" title="${I18N.t('common.edit')}" aria-label="${I18N.t('common.edit')}">✏️</button>
                <button class="btn btn-sm btn-danger" onclick="AdminPage.deleteProject(${p.id})" title="${I18N.t('common.delete')}" aria-label="${I18N.t('common.delete')}">🗑</button></td></tr>`).join('')}
            ${!items.length ? `<tr><td colspan="4" class="text-muted text-center">${I18N.t('admin.no_projects')}</td></tr>` : ''}
            </tbody></table>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    showProjectModal(id, name, quotaBytes) {
        const edit = !!id;
        const quotaGB = Math.round((quotaBytes || 5368709120) / (1024 ** 3) * 10) / 10;
        UI.showModal(edit ? I18N.t('admin.edit_project_title') : I18N.t('admin.new_project'),
            `<div class="form-group"><label>${I18N.t('admin.col_name')}</label><input type="text" id="proj-name" value="${UI.esc(name||'')}" class="form-control" placeholder="${I18N.t('admin.project_name_placeholder')}"></div>
             <div class="form-group"><label>${I18N.t('admin.quota_gb_label')}</label><input type="number" id="proj-quota" value="${quotaGB}" class="form-control" min="0.1" step="0.1"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="UI.busyClick(this,()=>AdminPage.saveProject(${id||'null'}))">${edit ? I18N.t('common.change') : I18N.t('common.create')}</button>`);
    },
    async saveProject(id) {
        const n = document.getElementById('proj-name').value.trim();
        const quotaGB = parseFloat(document.getElementById('proj-quota').value) || 5;
        const quotaBytes = Math.round(quotaGB * 1024 ** 3);
        if (!n) { UI.toast(I18N.t('app.name_required'), 'error'); return; }
        try {
            if (id) await API.admin.projects.update(id, n, quotaBytes); else await API.admin.projects.create(n, quotaBytes);
            UI.closeModal(); UI.toast(id ? I18N.t('admin.updated') : I18N.t('admin.created'), 'success'); this.switchTab('projects');
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    deleteProject(id) {
        UI.confirmAction(I18N.t('admin.delete_project_confirm_title'), I18N.t('admin.delete_project_confirm_body'), I18N.t('common.delete'), async () => {
            try { await API.admin.projects.delete(id); UI.toast(I18N.t('admin.deleted'), 'success'); this.switchTab('projects'); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    showBulkProjectQuota() {
        UI.showModal(I18N.t('admin.bulk_project_quota_title'), `
            <div class="form-group"><label>${I18N.t('admin.bulk_quota_new_label')}</label><input type="number" id="bulk-project-quota" class="form-control" value="5" min="0.1" step="0.1"></div>
            <p class="text-muted" style="font-size:.78rem">${I18N.t('admin.bulk_project_quota_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doBulkProjectQuota()">${I18N.t('common.change')}</button>`);
    },
    async doBulkProjectQuota() {
        const gb = parseFloat(document.getElementById('bulk-project-quota').value) || 0;
        if (gb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        try { await API.admin.projects.bulkQuota(Math.round(gb * 1024)); UI.closeModal(); UI.toast(I18N.t('admin.bulk_project_quota_done'), 'success'); this.switchTab('projects'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Project members (ACL) ── */
    async showMembersModal(projectId, projectName) {
        UI.showModal(I18N.t('admin.members_modal_title', { name: projectName }), `
            <div class="form-group">
                <label>${I18N.t('admin.add_member_label')}</label>
                <div style="display:flex;gap:6px">
                    <input type="text" id="member-search" class="form-control" placeholder="${I18N.t('admin.member_search_placeholder')}" oninput="AdminPage.searchMemberCandidates(${projectId})">
                    <select id="member-permission" class="form-control" style="width:140px">
                        <option value="view">${I18N.t('shares.perm_view_option')}</option>
                        <option value="edit">${I18N.t('shares.perm_edit_option')}</option>
                    </select>
                </div>
                <div id="member-search-results" class="member-search-results"></div>
            </div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <div id="member-list"><div class="spinner"></div></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.close')}</button>`);
        this._loadMembers(projectId);
    },

    async _loadMembers(projectId) {
        const el = document.getElementById('member-list');
        if (!el) return;
        try {
            const members = (await API.admin.projects.members.list(projectId)) || [];
            if (!members.length) { el.innerHTML = `<p class="text-muted" style="font-size:.82rem">${I18N.t('admin.no_members')}</p>`; return; }
            el.innerHTML = members.map(m => `
                <div class="member-row">
                    <div class="member-row-identity">
                        ${UI.avatarHTML(m.user_id, m.full_name || m.username)}
                        <div><strong>${UI.esc(m.full_name || m.username)}</strong> <span class="text-muted">@${UI.esc(m.username)}</span></div>
                    </div>
                    <div class="member-row-actions">
                        <select class="form-control" style="width:150px" onchange="AdminPage.changeMemberPermission(${projectId},${m.user_id},this.value)">
                            <option value="view" ${m.permission==='view'?'selected':''}>${I18N.t('shares.perm_view_option')}</option>
                            <option value="edit" ${m.permission==='edit'?'selected':''}>${I18N.t('shares.perm_edit_option')}</option>
                        </select>
                        <button class="btn btn-sm btn-danger" onclick="AdminPage.removeMember(${projectId},${m.user_id},${UI.escJson(m.full_name || m.username)})" title="${I18N.t('common.remove')}" aria-label="${I18N.t('common.remove')}">🗑</button>
                    </div>
                </div>`).join('');
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    _memberSearchTimer: null,
    searchMemberCandidates(projectId) {
        clearTimeout(this._memberSearchTimer);
        const q = document.getElementById('member-search').value.trim();
        const resEl = document.getElementById('member-search-results');
        if (q.length < 2) { resEl.innerHTML = ''; return; }
        this._memberSearchTimer = setTimeout(async () => {
            try {
                const users = (await API.sharing.searchUsers(q)) || [];
                resEl.innerHTML = users.map(u => `
                    <div class="member-search-item" onclick="AdminPage.addMember(${projectId},${u.id})">
                        ${UI.avatarHTML(u.id, u.full_name || u.username)}
                        <div><strong>${UI.esc(u.full_name || u.username)}</strong> <span class="text-muted">@${UI.esc(u.username)}</span></div>
                    </div>`).join('') || `<div class="text-muted" style="font-size:.8rem;padding:4px 0">${I18N.t('shares.no_results')}</div>`;
            } catch { resEl.innerHTML = ''; }
        }, 250);
    },

    async addMember(projectId, userId) {
        const permission = document.getElementById('member-permission').value;
        try {
            await API.admin.projects.members.add(projectId, userId, permission);
            document.getElementById('member-search').value = '';
            document.getElementById('member-search-results').innerHTML = '';
            UI.toast(I18N.t('admin.member_added'), 'success');
            this._loadMembers(projectId);
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    async changeMemberPermission(projectId, userId, permission) {
        try { await API.admin.projects.members.update(projectId, userId, permission); UI.toast(I18N.t('admin.updated'), 'success'); } catch (e) { UI.toast(e.message, 'error'); }
    },
    removeMember(projectId, userId, name) {
        UI.confirmAction(I18N.t('admin.remove_member_title'), I18N.t('admin.remove_member_confirm_body', { name: name || '' }), I18N.t('common.remove'), async () => {
            try { await API.admin.projects.members.remove(projectId, userId); UI.toast(I18N.t('admin.member_removed'), 'success'); this._loadMembers(projectId); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    /* ── Users ── */
    // Selected rows for the bulk-quota/bulk-delete bar — the middle ground
    // between "one user at a time" and the pre-existing "literally
    // everyone" actions. Admin rows never get a checkbox at all (see
    // userRowHTML): bulk quota silently no-ops on them server-side anyway
    // (SetUsersQuota is scoped to role='user'), and bulk-deleting an admin
    // is rare/dangerous enough to keep on the single-user delete flow only.
    _selectedUserIds: new Set(),

    async renderUsers(el) {
        try {
            const users = (await API.admin.users.list()) || [];
            this._selectedUserIds = new Set();
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_users')}</h2>
                <div style="display:flex;gap:8px;align-items:center">
                    <input type="text" id="admin-user-search" class="form-control" placeholder="${I18N.t('files.search_placeholder')}" style="width:200px" oninput="AdminPage.filterUsers(this.value)">
                    <button class="btn btn-ghost btn-sm" onclick="AdminPage.showBulkUserQuota()">📊 ${I18N.t('admin.bulk_quota_all')}</button>
                    <button class="btn btn-danger btn-sm" onclick="AdminPage.confirmDeleteAllUsers()">🗑 ${I18N.t('admin.delete_all_button')}</button>
                    <button class="btn btn-ghost btn-sm" onclick="AdminPage.showImportModal()">📥 ${I18N.t('admin.import_button')}</button>
                    <button class="btn btn-primary btn-sm" onclick="AdminPage.showCreateUserModal()">${UI.icons.plus} ${I18N.t('admin.new_button')}</button>
                </div>
            </div>
            <div id="admin-user-bulk-bar" class="bulk-actions-bar hidden"></div>
            <table class="admin-table" id="admin-users-table"><thead><tr>
                <th><input type="checkbox" id="admin-user-select-all" onchange="AdminPage.toggleSelectAllUsers(this.checked)" aria-label="${I18N.t('files.select_all')}"></th>
                <th>${I18N.t('admin.col_id')}</th><th>${I18N.t('admin.col_name')}</th><th>${I18N.t('admin.col_username')}</th><th>${I18N.t('admin.col_role')}</th><th>${I18N.t('admin.col_quota')}</th><th>${I18N.t('admin.col_actions')}</th></tr></thead><tbody>
            ${users.map(u => this.userRowHTML(u)).join('')}
            ${!users.length ? `<tr><td colspan="7" class="text-muted text-center">${I18N.t('admin.no_employees')}</td></tr>` : ''}
            </tbody></table>`;
            this._users = users;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    userRowHTML(u) {
        const checkbox = u.role === 'admin' ? '' :
            `<input type="checkbox" ${this._selectedUserIds.has(u.id) ? 'checked' : ''} onchange="AdminPage.toggleSelectUser(${u.id},this.checked)" aria-label="${I18N.t('files.select_item')}">`;
        return `<tr data-uid="${u.id}"><td>${checkbox}</td><td>${u.id}</td><td><div class="table-identity">${UI.avatarHTML(u.id, u.full_name, 'share-user-avatar-sm')}<span>${UI.esc(u.full_name)}</span> ${u.must_change_password ? `<span class="badge" title="${I18N.t('admin.force_pw_badge_title')}">🔑</span>` : ''}</div></td><td>@${UI.esc(u.username)}</td>
                <td><span class="badge badge-${u.role === 'admin' ? 'admin' : 'user'}">${u.role === 'admin' ? I18N.t('app.role_admin') : I18N.t('app.role_user')}</span></td>
                <td>${UI.formatBytes(u.quota_bytes || 0)}</td>
                <td><button class="btn btn-sm btn-ghost" onclick="AdminPage.showEditUserModal(${u.id})" title="${I18N.t('common.edit')}" aria-label="${I18N.t('common.edit')}">✏️</button>
                ${u.role !== 'admin' ? `<button class="btn btn-sm btn-ghost" onclick="AdminPage.impersonate(${u.id},${UI.escJson(u.full_name || u.username)})" title="${I18N.t('admin.impersonate_button')}" aria-label="${I18N.t('admin.impersonate_button')}">🎭</button>` : ''}
                ${u.role !== 'admin' ? `<button class="btn btn-sm btn-danger" onclick="AdminPage.deleteUser(${u.id})" title="${I18N.t('common.delete')}" aria-label="${I18N.t('common.delete')}">🗑</button>` : ''}</td></tr>`;
    },

    toggleSelectUser(id, checked) {
        if (checked) this._selectedUserIds.add(id); else this._selectedUserIds.delete(id);
        this.renderUserBulkBar();
    },

    toggleSelectAllUsers(checked) {
        this._selectedUserIds = new Set(checked ? (this._users || []).filter(u => u.role !== 'admin').map(u => u.id) : []);
        document.querySelectorAll('#admin-users-table tbody input[type=checkbox]').forEach(cb => { cb.checked = checked; });
        this.renderUserBulkBar();
    },

    renderUserBulkBar() {
        const bar = document.getElementById('admin-user-bulk-bar');
        if (!bar) return;
        const count = this._selectedUserIds.size;
        if (!count) { bar.classList.add('hidden'); bar.innerHTML = ''; return; }
        bar.classList.remove('hidden');
        bar.innerHTML = `
            <span class="bulk-bar-count">${I18N.tn('files.selected_count', count)}</span>
            <div class="bulk-bar-actions">
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.showBulkUserQuota(true)">📊 ${I18N.t('admin.bulk_quota_selected')}</button>
                <button class="btn btn-ghost btn-sm btn-danger" onclick="AdminPage.confirmBulkDeleteSelected()">🗑 ${I18N.t('admin.bulk_delete_selected')}</button>
                <button class="btn btn-icon btn-ghost btn-sm" onclick="AdminPage.toggleSelectAllUsers(false)" title="${I18N.t('files.clear_selection')}" aria-label="${I18N.t('files.clear_selection')}">✕</button>
            </div>`;
    },

    confirmBulkDeleteSelected() {
        const ids = Array.from(this._selectedUserIds);
        if (!ids.length) return;
        UI.confirmAction(I18N.t('admin.bulk_delete_selected'), I18N.tn('admin.bulk_delete_selected_confirm', ids.length, { count: ids.length }), I18N.t('common.delete'), async () => {
            try {
                const res = await API.admin.users.bulkDelete(ids);
                UI.toast(I18N.tn('admin.bulk_delete_selected_done', res.deleted, { count: res.deleted }), 'success');
                if (res.skipped && res.skipped.length) {
                    UI.toast(I18N.t('admin.bulk_delete_skipped_admins', { names: res.skipped.join(', ') }), 'info');
                }
                this.switchTab('users');
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    filterUsers(q) {
        const lc = q.toLowerCase();
        document.querySelectorAll('#admin-users-table tbody tr').forEach(r => { r.style.display = r.textContent.toLowerCase().includes(lc) ? '' : 'none'; });
    },

    /* ── Audit log ── */
    async renderAuditLog(el) {
        try {
            const entries = (await API.admin.auditLog()) || [];
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_audit_log')}</h2>
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.exportAuditLog()">${UI.icons.download} ${I18N.t('admin.export_csv')}</button>
            </div>
            <table class="admin-table"><thead><tr><th>${I18N.t('admin.col_time')}</th><th>${I18N.t('admin.col_who')}</th><th>${I18N.t('admin.col_action')}</th><th>${I18N.t('admin.col_target')}</th><th>${I18N.t('admin.col_details')}</th></tr></thead><tbody>
            ${entries.map(e => `<tr>
                <td class="text-muted" style="white-space:nowrap">${new Date(e.created_at).toLocaleString(I18N.dateLocale())}</td>
                <td>${UI.esc(e.actor_name || '—')}</td>
                <td><code>${UI.esc(e.action)}</code></td>
                <td>${UI.esc(e.target_name || (e.target_type ? e.target_type + ' #' + e.target_id : '—'))}</td>
                <td class="text-muted" style="font-size:.75rem">${e.details ? UI.esc(JSON.stringify(e.details)) : ''}</td>
            </tr>`).join('')}
            ${!entries.length ? `<tr><td colspan="5" class="text-muted text-center">${I18N.t('admin.no_entries')}</td></tr>` : ''}
            </tbody></table>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    exportAuditLog() {
        const a = document.createElement('a');
        a.href = API.admin.auditLogExportURL();
        a.download = 'paylash-audit-log.csv';
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
    },

    /* ── Active large uploads ── */
    async renderUploads(el) {
        try {
            const sessions = (await API.admin.uploads.list()) || [];
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_uploads')}</h2>
                <p class="text-muted" style="font-size:.8rem">${I18N.t('admin.uploads_hint')}</p>
            </div>
            <table class="admin-table"><thead><tr><th>${I18N.t('admin.col_file')}</th><th>${I18N.t('app.role_user')}</th><th>${I18N.t('files.col_size')}</th><th>${I18N.t('admin.col_parts')}</th><th>${I18N.t('admin.col_location')}</th><th>${I18N.t('admin.col_last_activity')}</th><th></th></tr></thead><tbody>
            ${sessions.map(s => `<tr>
                <td>${UI.esc(s.file_name)}</td>
                <td>${UI.esc(s.owner_display_name || s.owner_username)}</td>
                <td>${UI.formatBytes(s.total_size)}</td>
                <td>${s.part_count}</td>
                <td>${UI.esc(s.scope)}</td>
                <td class="text-muted">${UI.formatDate(s.updated_at)}</td>
                <td><button class="btn btn-sm btn-danger" onclick="AdminPage.abortUpload('${s.id}')">${I18N.t('common.cancel')}</button></td>
            </tr>`).join('')}
            ${!sessions.length ? `<tr><td colspan="7" class="text-muted text-center">${I18N.t('admin.no_active_uploads')}</td></tr>` : ''}
            </tbody></table>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    /* ── Chat moderation reports ──
       The ONLY window this admin panel has into chat content — each row is
       a reporter-initiated, timestamped snapshot (see ChatReportView
       server-side), never a live view of a conversation. Resolving/
       dismissing a report doesn't grant any further access to its source
       conversation. */
    async renderChatReports(el) {
        try {
            const status = this._reportStatusFilter || 'open';
            const reports = (await API.admin.chatReports.list(status)) || [];
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_chat_reports')}</h2>
                <select id="report-status-filter" class="form-control" style="width:auto" onchange="AdminPage.filterChatReports(this.value)">
                    <option value="open" ${status === 'open' ? 'selected' : ''}>${I18N.t('admin.report_status_open')}</option>
                    <option value="resolved" ${status === 'resolved' ? 'selected' : ''}>${I18N.t('admin.report_status_resolved')}</option>
                    <option value="dismissed" ${status === 'dismissed' ? 'selected' : ''}>${I18N.t('admin.report_status_dismissed')}</option>
                    <option value="all" ${status === 'all' ? 'selected' : ''}>${I18N.t('admin.report_status_all')}</option>
                </select>
            </div>
            <div class="report-list">
                ${reports.map(r => this.reportCardHTML(r)).join('')}
                ${!reports.length ? `<p class="text-muted text-center">${I18N.t('admin.no_reports')}</p>` : ''}
            </div>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    filterChatReports(status) {
        this._reportStatusFilter = status;
        this.switchTab('chat-reports');
    },

    reportCardHTML(r) {
        const conv = r.conversation_type === 'group'
            ? I18N.t('admin.report_group_label', { name: r.conversation_label || I18N.t('chat.unnamed_group') })
            : I18N.t('admin.report_dm_label');
        const target = r.reported_user_name ? I18N.t('admin.report_against', { name: r.reported_user_name }) : '';
        return `<div class="report-card">
            <div class="report-card-head">
                <span>${I18N.t('admin.report_by', { name: r.reporter_name || I18N.t('admin.report_unknown_user') })} · ${UI.esc(conv)}</span>
                <span class="text-muted" style="font-size:.78rem">${UI.formatDate(r.created_at)}</span>
            </div>
            ${target ? `<div class="text-muted" style="font-size:.82rem">${UI.esc(target)}</div>` : ''}
            ${r.message_body_snapshot ? `<div class="report-card-snapshot">${UI.esc(r.message_body_snapshot)}</div>` : ''}
            ${r.reason ? `<div class="report-card-reason">"${UI.esc(r.reason)}"</div>` : ''}
            <div class="report-card-status">
                ${r.status === 'open'
                    ? `<button class="btn btn-sm btn-primary" onclick="AdminPage.resolveChatReport(${r.id},'resolved')">${I18N.t('admin.report_resolve')}</button>
                       <button class="btn btn-sm btn-ghost" onclick="AdminPage.resolveChatReport(${r.id},'dismissed')">${I18N.t('admin.report_dismiss')}</button>`
                    : `<span class="chip ${r.status}">${I18N.t('admin.report_status_' + r.status)}${r.resolved_by_name ? ' · ' + UI.esc(r.resolved_by_name) : ''}</span>`}
            </div>
        </div>`;
    },

    async resolveChatReport(id, action) {
        try {
            await API.admin.chatReports.resolve(id, action);
            UI.toast(I18N.t('admin.report_updated'), 'success');
            this.switchTab('chat-reports');
            this.refreshOpenReportCount();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    abortUpload(id) {
        UI.confirmAction(I18N.t('admin.upload_cancel_confirm_title'), I18N.t('admin.upload_cancel_confirm_body'), I18N.t('common.cancel'), async () => {
            try {
                await API.admin.uploads.abort(id);
                UI.toast(I18N.t('admin.upload_cancelled'), 'success');
                this.switchTab('uploads');
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    showCreateUserModal() {
        UI.showModal(I18N.t('admin.new_employee_title'), `
            <div class="form-group"><label>${I18N.t('auth.fullname_label')}</label><input type="text" id="nu-name" class="form-control" placeholder="${I18N.t('auth.fullname_placeholder')}"></div>
            <div class="form-group"><label>${I18N.t('auth.username_label')}</label><input type="text" id="nu-username" class="form-control" placeholder="${I18N.t('admin.username_field_placeholder')}"></div>
            <div class="form-group"><label>${I18N.t('auth.password_label')}</label>${UI.passwordField('nu-password', I18N.t('auth.password_min_placeholder'))}</div>
            <div class="form-group"><label>${I18N.t('admin.col_role')}</label><select id="nu-role" class="form-control"><option value="user">${I18N.t('app.role_user')}</option><option value="admin">${I18N.t('app.role_admin')}</option></select></div>
            <div class="form-group"><label>${I18N.t('admin.quota_gb_label')}</label><input type="number" id="nu-quota" class="form-control" value="10" min="0" step="0.1"></div>
            <p class="text-muted" style="font-size:.78rem">${I18N.t('admin.project_membership_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="UI.busyClick(this,()=>AdminPage.doCreateUser())">${I18N.t('common.create')}</button>`);
    },

    async doCreateUser() {
        const name = document.getElementById('nu-name').value.trim();
        const username = document.getElementById('nu-username').value.trim();
        const password = document.getElementById('nu-password').value;
        const role = document.getElementById('nu-role').value;
        const quotaMB = Math.round((parseFloat(document.getElementById('nu-quota').value) || 0) * 1024);
        if (!name || !username || !password) { UI.toast(I18N.t('auth.fill_all_fields'), 'error'); return; }
        try {
            await API.admin.users.create({ full_name: name, username, password, role, quota_mb: quotaMB });
            UI.closeModal(); UI.toast(I18N.t('admin.employee_created'), 'success'); this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    showEditUserModal(id) {
        const u = this._users.find(x => x.id === id); if (!u) return;
        const gb = Math.round((u.quota_bytes || 0) / (1024 ** 3) * 10) / 10;
        UI.showModal(I18N.t('admin.edit_user_title'), `
            <div class="form-group"><label>${I18N.t('auth.fullname_label')}</label><input type="text" id="eu-name" value="${UI.esc(u.full_name)}" class="form-control"></div>
            <div class="form-group"><label>${I18N.t('app.new_password_label')}</label>${UI.passwordField('eu-password', I18N.t('admin.new_password_optional_placeholder'))}</div>
            <div class="form-group"><label>${I18N.t('admin.col_role')}</label><select id="eu-role" class="form-control"><option value="user" ${u.role==='user'?'selected':''}>${I18N.t('app.role_user')}</option><option value="admin" ${u.role==='admin'?'selected':''}>${I18N.t('app.role_admin')}</option></select></div>
            <div class="form-group"><label>${I18N.t('admin.quota_gb_label')}</label><input type="number" id="eu-quota" value="${gb}" class="form-control" min="0" step="0.1"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="UI.busyClick(this,()=>AdminPage.saveUser(${id}))">${I18N.t('common.save')}</button>`);
    },
    async saveUser(id) {
        const role = document.getElementById('eu-role').value;
        const gb = parseFloat(document.getElementById('eu-quota').value) || 0;
        const name = document.getElementById('eu-name').value.trim();
        const password = document.getElementById('eu-password').value;
        const data = { role, quota_bytes: Math.round(gb * 1024 ** 3) };
        if (name) data.display_name = name;
        if (password) data.password = password;
        try { await API.admin.users.update(id, data); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
    },
    deleteUser(id) {
        UI.confirmAction(I18N.t('admin.delete_employee_confirm_title'), I18N.t('admin.delete_employee_confirm_body'), I18N.t('common.delete'), async () => {
            try { await API.admin.users.delete(id); UI.toast(I18N.t('admin.deleted'), 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    // Full page reload after switching sessions rather than trying to patch
    // App.user and every already-loaded page's cached state in place — this
    // is a genuine identity switch (new permissions, new files, new chat
    // history), and a reload is the only way to guarantee nothing from the
    // admin's own session lingers in memory.
    impersonate(id, name) {
        UI.confirmAction(I18N.t('admin.impersonate_confirm_title'), I18N.t('admin.impersonate_confirm_body', { name }), I18N.t('admin.impersonate_button'), async () => {
            try {
                await API.admin.users.impersonate(id);
                location.reload();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    confirmDeleteAllUsers() {
        const word = I18N.t('admin.delete_all_confirm_word');
        UI.showModal(I18N.t('admin.delete_all_title'), `
            <p style="color:var(--danger);font-weight:600">${I18N.t('admin.delete_all_warning')}</p>
            <p class="text-muted" style="font-size:.85rem">${I18N.t('admin.delete_all_hint', { word })}</p>
            <div class="form-group"><input type="text" id="confirm-delete-all" class="form-control" placeholder="${I18N.t('admin.delete_all_placeholder', { word })}"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-danger" onclick="AdminPage.doDeleteAllUsers()">${I18N.t('admin.delete_all_button')}</button>`);
    },
    async doDeleteAllUsers() {
        const word = I18N.t('admin.delete_all_confirm_word');
        if (document.getElementById('confirm-delete-all').value.trim() !== word) { UI.toast(I18N.t('admin.delete_all_confirm_error', { word }), 'error'); return; }
        try { const res = await API.admin.users.deleteAll(); UI.closeModal(); UI.toast(I18N.t('admin.delete_all_done', { count: res.deleted }), 'success'); this.switchTab('users'); }
        catch (e) { UI.toast(e.message, 'error'); }
    },

    // selectedOnly=true scopes the modal (and doBulkUserQuota below) to
    // this._selectedUserIds instead of every employee — same modal, same
    // submit handler, just remembers which mode it's in via a data
    // attribute rather than two near-duplicate copies of both.
    showBulkUserQuota(selectedOnly) {
        const count = this._selectedUserIds.size;
        UI.showModal(selectedOnly ? I18N.t('admin.bulk_quota_selected') : I18N.t('admin.bulk_user_quota_title'), `
            <div class="form-group"><label>${I18N.t('admin.bulk_quota_new_label')}</label><input type="number" id="bulk-user-quota" class="form-control" value="10" min="0.1" step="0.1"></div>
            <p class="text-muted" style="font-size:.78rem">${selectedOnly ? I18N.tn('admin.bulk_quota_selected_hint', count, { count }) : I18N.t('admin.bulk_user_quota_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doBulkUserQuota(${!!selectedOnly})">${I18N.t('common.change')}</button>`);
    },
    async doBulkUserQuota(selectedOnly) {
        const gb = parseFloat(document.getElementById('bulk-user-quota').value) || 0;
        if (gb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        const ids = selectedOnly ? Array.from(this._selectedUserIds) : null;
        try {
            await API.admin.users.bulkQuota(Math.round(gb * 1024), ids);
            UI.closeModal();
            UI.toast(I18N.t('admin.bulk_user_quota_done'), 'success');
            this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    showImportModal() {
        UI.showModal(I18N.t('admin.import_title'), `
            <p class="text-muted" style="font-size:.82rem;margin-bottom:12px">${I18N.t('admin.import_format_hint')}<br>
            <code style="font-size:.75rem">username, password, full_name, quota_mb</code></p>
            <div class="form-group">
                <input type="file" id="import-file" class="form-control" accept=".csv,.xlsx,.xls">
            </div>
            <div id="import-results" style="display:none;max-height:200px;overflow:auto;margin-top:8px"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" id="import-btn" onclick="AdminPage.doImportUsers()">${I18N.t('admin.import_submit')}</button>`);
    },

    async doImportUsers() {
        const fileInput = document.getElementById('import-file');
        const file = fileInput?.files[0];
        if (!file) { UI.toast(I18N.t('admin.import_choose_file'), 'error'); return; }
        const btn = document.getElementById('import-btn');
        btn.disabled = true; btn.textContent = I18N.t('common.loading');
        try {
            const result = await API.admin.users.importFile(file);
            const el = document.getElementById('import-results');
            el.style.display = 'block';
            let html = `<p style="font-weight:600;margin-bottom:6px">${I18N.t('admin.import_result_summary', { created: result.created, total: result.total })}</p>`;
            if (result.results) {
                html += '<div style="font-size:.78rem">';
                result.results.forEach(r => {
                    html += `<div style="padding:2px 0;color:${r.success ? 'var(--success)' : 'var(--danger)'}">${UI.esc(r.username)}: ${r.success ? I18N.t('admin.import_row_created') : '✕ ' + UI.esc(r.error)}</div>`;
                });
                html += '</div>';
            }
            el.innerHTML = html;
            if (result.created > 0) this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
        finally { btn.disabled = false; btn.textContent = I18N.t('admin.import_submit'); }
    }
};
