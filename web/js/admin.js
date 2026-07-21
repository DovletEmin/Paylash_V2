/* Paylash — Admin Panel */
const AdminPage = {
    currentTab: 'dashboard',
    _users: [],
    _projects: [],
    _adminProjectFiles: { projectId: null, folderId: null, files: [], folders: [], breadcrumbs: [] },
    _adminCommonFiles: { folderId: null, files: [], folders: [], breadcrumbs: [] },

    render() {
        return `
        <div class="admin-page">
            <div class="admin-sidebar">
                <div class="admin-title">${UI.icons.settings} ${I18N.t('app.nav_admin_section')}</div>
                <nav class="admin-nav">
                    <a class="admin-nav-item ${this.currentTab === 'dashboard' ? 'active' : ''}" onclick="AdminPage.switchTab('dashboard')">${UI.icons.dashboard} ${I18N.t('admin.nav_dashboard')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'projects' ? 'active' : ''}" onclick="AdminPage.switchTab('projects')">${UI.icons.users} ${I18N.t('admin.nav_projects')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'users' ? 'active' : ''}" onclick="AdminPage.switchTab('users')">${UI.icons.user} ${I18N.t('admin.nav_users')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'project-files' ? 'active' : ''}" onclick="AdminPage.switchTab('project-files')">📁 ${I18N.t('admin.nav_project_files')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'common-files' ? 'active' : ''}" onclick="AdminPage.switchTab('common-files')">🌐 ${I18N.t('admin.nav_common_files')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'audit-log' ? 'active' : ''}" onclick="AdminPage.switchTab('audit-log')">🕓 ${I18N.t('admin.nav_audit_log')}</a>
                    <a class="admin-nav-item ${this.currentTab === 'uploads' ? 'active' : ''}" onclick="AdminPage.switchTab('uploads')">⬆ ${I18N.t('admin.nav_uploads')}</a>
                </nav>
            </div>
            <div class="admin-content" id="admin-content"></div>
        </div>`;
    },

    async init() { await this.switchTab(this.currentTab); },

    async switchTab(tab) {
        this.currentTab = tab;
        document.querySelectorAll('.admin-nav-item').forEach((el, i) => {
            el.classList.toggle('active', ['dashboard','projects','users','project-files','common-files','audit-log','uploads'][i] === tab);
        });
        const c = document.getElementById('admin-content');
        if (!c) return;
        c.innerHTML = '<div class="admin-loading"><div class="spinner"></div></div>';
        switch (tab) {
            case 'dashboard':     await this.renderDashboard(c); break;
            case 'projects':      await this.renderProjects(c); break;
            case 'users':         await this.renderUsers(c); break;
            case 'project-files': await this.renderProjectFiles(c); break;
            case 'common-files':  await this.renderCommonFiles(c); break;
            case 'audit-log':     await this.renderAuditLog(c); break;
            case 'uploads':       await this.renderUploads(c); break;
        }
    },

    /* ── Dashboard ── */
    async renderDashboard(el) {
        try {
            const [d, pq] = await Promise.all([API.admin.dashboard(), API.admin.publicQuota.get()]);
            const pqMB = Math.round((pq.quota_bytes || 53687091200) / (1024 * 1024));
            el.innerHTML = `
            <h2 style="font-size:1.1rem;font-weight:600;margin-bottom:16px">${I18N.t('admin.nav_dashboard')}</h2>
            <div class="stat-cards">
                <div class="stat-card"><div class="stat-card-value">${d.total_users || 0}</div><div class="stat-card-label">${I18N.t('admin.nav_users')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_projects || 0}</div><div class="stat-card-label">${I18N.t('admin.nav_projects')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_files || 0}</div><div class="stat-card-label">${I18N.t('app.nav_files')}</div></div>
                <div class="stat-card"><div class="stat-card-value">${UI.formatBytes(d.total_bytes || 0)}</div><div class="stat-card-label">${I18N.t('admin.stat_used_space')}</div></div>
            </div>
            <h3 style="font-size:1rem;font-weight:600;margin:24px 0 12px">${I18N.t('admin.public_quota_title')}</h3>
            <div style="display:flex;align-items:center;gap:10px">
                <input type="number" id="public-quota-mb" class="form-control" value="${pqMB}" min="1" style="width:160px">
                <span class="text-muted" style="font-size:.82rem">MB</span>
                <button class="btn btn-primary btn-sm" onclick="AdminPage.savePublicQuota()">${I18N.t('common.save')}</button>
            </div>
            <p class="text-muted" style="font-size:.78rem;margin-top:6px">${I18N.t('admin.public_quota_hint')}</p>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    async savePublicQuota() {
        const mb = parseInt(document.getElementById('public-quota-mb').value) || 0;
        if (mb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        try { await API.admin.publicQuota.set(mb); UI.toast(I18N.t('admin.public_quota_changed'), 'success'); } catch (e) { UI.toast(e.message, 'error'); }
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
                <button class="btn btn-sm btn-ghost" onclick="AdminPage.showProjectModal(${p.id},${UI.escJson(p.name)},${p.quota_bytes||0})">✏️</button>
                <button class="btn btn-sm btn-danger" onclick="AdminPage.deleteProject(${p.id})">🗑</button></td></tr>`).join('')}
            ${!items.length ? `<tr><td colspan="4" class="text-muted text-center">${I18N.t('admin.no_projects')}</td></tr>` : ''}
            </tbody></table>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    showProjectModal(id, name, quotaBytes) {
        const edit = !!id;
        const quotaMB = Math.round((quotaBytes || 5368709120) / (1024 * 1024));
        UI.showModal(edit ? I18N.t('admin.edit_project_title') : I18N.t('admin.new_project'),
            `<div class="form-group"><label>${I18N.t('admin.col_name')}</label><input type="text" id="proj-name" value="${name||''}" class="form-control" placeholder="${I18N.t('admin.project_name_placeholder')}"></div>
             <div class="form-group"><label>${I18N.t('admin.quota_mb_label')}</label><input type="number" id="proj-quota" value="${quotaMB}" class="form-control" min="1"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.saveProject(${id||'null'})">${edit ? I18N.t('common.change') : I18N.t('common.create')}</button>`);
    },
    async saveProject(id) {
        const n = document.getElementById('proj-name').value.trim();
        const quotaMB = parseInt(document.getElementById('proj-quota').value) || 5120;
        const quotaBytes = quotaMB * 1024 * 1024;
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
            <div class="form-group"><label>${I18N.t('admin.bulk_quota_new_label')}</label><input type="number" id="bulk-project-quota" class="form-control" value="5120" min="1"></div>
            <p class="text-muted" style="font-size:.78rem">${I18N.t('admin.bulk_project_quota_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doBulkProjectQuota()">${I18N.t('common.change')}</button>`);
    },
    async doBulkProjectQuota() {
        const mb = parseInt(document.getElementById('bulk-project-quota').value) || 0;
        if (mb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        try { await API.admin.projects.bulkQuota(mb); UI.closeModal(); UI.toast(I18N.t('admin.bulk_project_quota_done'), 'success'); this.switchTab('projects'); } catch (e) { UI.toast(e.message, 'error'); }
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
                <div class="member-row" style="display:flex;align-items:center;justify-content:space-between;padding:6px 0">
                    <div><strong>${UI.esc(m.full_name || m.username)}</strong> <span class="text-muted">@${UI.esc(m.username)}</span></div>
                    <div style="display:flex;gap:6px;align-items:center">
                        <select class="form-control" style="width:150px" onchange="AdminPage.changeMemberPermission(${projectId},${m.user_id},this.value)">
                            <option value="view" ${m.permission==='view'?'selected':''}>${I18N.t('shares.perm_view_option')}</option>
                            <option value="edit" ${m.permission==='edit'?'selected':''}>${I18N.t('shares.perm_edit_option')}</option>
                        </select>
                        <button class="btn btn-sm btn-danger" onclick="AdminPage.removeMember(${projectId},${m.user_id})">🗑</button>
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
                        <strong>${UI.esc(u.full_name || u.username)}</strong> <span class="text-muted">@${UI.esc(u.username)}</span>
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
    async removeMember(projectId, userId) {
        try { await API.admin.projects.members.remove(projectId, userId); UI.toast(I18N.t('admin.member_removed'), 'success'); this._loadMembers(projectId); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Users ── */
    async renderUsers(el) {
        try {
            const users = (await API.admin.users.list()) || [];
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
            <table class="admin-table" id="admin-users-table"><thead><tr><th>${I18N.t('admin.col_id')}</th><th>${I18N.t('admin.col_name')}</th><th>${I18N.t('admin.col_username')}</th><th>${I18N.t('admin.col_role')}</th><th>${I18N.t('admin.col_quota')}</th><th>${I18N.t('admin.col_actions')}</th></tr></thead><tbody>
            ${users.map(u => `<tr data-uid="${u.id}"><td>${u.id}</td><td>${UI.esc(u.full_name)} ${u.must_change_password ? `<span class="badge" title="${I18N.t('admin.force_pw_badge_title')}">🔑</span>` : ''}</td><td>@${UI.esc(u.username)}</td>
                <td><span class="badge badge-${u.role === 'admin' ? 'admin' : 'user'}">${u.role === 'admin' ? I18N.t('app.role_admin') : I18N.t('app.role_user')}</span></td>
                <td>${UI.formatBytes(u.quota_bytes || 0)}</td>
                <td><button class="btn btn-sm btn-ghost" onclick="AdminPage.showEditUserModal(${u.id})">✏️</button>
                ${u.role !== 'admin' ? `<button class="btn btn-sm btn-danger" onclick="AdminPage.deleteUser(${u.id})">🗑</button>` : ''}</td></tr>`).join('')}
            ${!users.length ? `<tr><td colspan="6" class="text-muted text-center">${I18N.t('admin.no_employees')}</td></tr>` : ''}
            </tbody></table>`;
            this._users = users;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
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
            <div class="admin-header"><h2>${I18N.t('admin.nav_audit_log')}</h2></div>
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
            <div class="form-group"><label>${I18N.t('admin.quota_mb_label')}</label><input type="number" id="nu-quota" class="form-control" value="10240" min="0"></div>
            <p class="text-muted" style="font-size:.78rem">${I18N.t('admin.project_membership_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doCreateUser()">${I18N.t('common.create')}</button>`);
    },

    async doCreateUser() {
        const name = document.getElementById('nu-name').value.trim();
        const username = document.getElementById('nu-username').value.trim();
        const password = document.getElementById('nu-password').value;
        const role = document.getElementById('nu-role').value;
        const quotaMB = parseInt(document.getElementById('nu-quota').value) || 0;
        if (!name || !username || !password) { UI.toast(I18N.t('auth.fill_all_fields'), 'error'); return; }
        try {
            await API.admin.users.create({ full_name: name, username, password, role, quota_mb: quotaMB });
            UI.closeModal(); UI.toast(I18N.t('admin.employee_created'), 'success'); this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    showEditUserModal(id) {
        const u = this._users.find(x => x.id === id); if (!u) return;
        const mb = Math.round((u.quota_bytes || 0) / (1024 * 1024));
        UI.showModal(I18N.t('admin.edit_user_title'), `
            <div class="form-group"><label>${I18N.t('auth.fullname_label')}</label><input type="text" id="eu-name" value="${UI.esc(u.full_name)}" class="form-control"></div>
            <div class="form-group"><label>${I18N.t('app.new_password_label')}</label>${UI.passwordField('eu-password', I18N.t('admin.new_password_optional_placeholder'))}</div>
            <div class="form-group"><label>${I18N.t('admin.col_role')}</label><select id="eu-role" class="form-control"><option value="user" ${u.role==='user'?'selected':''}>${I18N.t('app.role_user')}</option><option value="admin" ${u.role==='admin'?'selected':''}>${I18N.t('app.role_admin')}</option></select></div>
            <div class="form-group"><label>${I18N.t('admin.quota_mb_label')}</label><input type="number" id="eu-quota" value="${mb}" class="form-control" min="0"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.saveUser(${id})">${I18N.t('common.save')}</button>`);
    },
    async saveUser(id) {
        const role = document.getElementById('eu-role').value;
        const mb = parseInt(document.getElementById('eu-quota').value) || 0;
        const name = document.getElementById('eu-name').value.trim();
        const password = document.getElementById('eu-password').value;
        const data = { role, quota_bytes: mb * 1024 * 1024 };
        if (name) data.display_name = name;
        if (password) data.password = password;
        try { await API.admin.users.update(id, data); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
    },
    deleteUser(id) {
        UI.confirmAction(I18N.t('admin.delete_employee_confirm_title'), I18N.t('admin.delete_employee_confirm_body'), I18N.t('common.delete'), async () => {
            try { await API.admin.users.delete(id); UI.toast(I18N.t('admin.deleted'), 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
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

    showBulkUserQuota() {
        UI.showModal(I18N.t('admin.bulk_user_quota_title'), `
            <div class="form-group"><label>${I18N.t('admin.bulk_quota_new_label')}</label><input type="number" id="bulk-user-quota" class="form-control" value="10240" min="1"></div>
            <p class="text-muted" style="font-size:.78rem">${I18N.t('admin.bulk_user_quota_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doBulkUserQuota()">${I18N.t('common.change')}</button>`);
    },
    async doBulkUserQuota() {
        const mb = parseInt(document.getElementById('bulk-user-quota').value) || 0;
        if (mb <= 0) { UI.toast(I18N.t('admin.invalid_quota'), 'error'); return; }
        try { await API.admin.users.bulkQuota(mb); UI.closeModal(); UI.toast(I18N.t('admin.bulk_user_quota_done'), 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
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
    },

    /* ── Project Files (admin browses any project's storage) ── */
    async renderProjectFiles(el) {
        try {
            const projects = (await API.admin.projects.list()) || [];
            this._projects = projects;
            const opts = projects.map(p => `<option value="${p.id}">${UI.esc(p.name)}</option>`).join('');
            el.innerHTML = `
            <div class="admin-header"><h2>${I18N.t('admin.nav_project_files')}</h2></div>
            <div style="display:flex;gap:10px;margin-bottom:16px;flex-wrap:wrap;align-items:end">
                <div class="form-group" style="margin:0"><label style="font-size:.78rem">${I18N.t('app.project_label')}</label><select id="pjf-project" class="form-control" style="width:220px" onchange="AdminPage.onPjfProjectChange()"><option value="">${I18N.t('admin.select_placeholder')}</option>${opts}</select></div>
            </div>
            <div id="pjf-actions" style="display:none;margin-bottom:12px">
                <button class="btn btn-primary btn-sm" onclick="document.getElementById('pjf-file-input').click()">${UI.icons.upload} ${I18N.t('admin.upload_file_button')}</button>
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.showPjfNewFolder()">${UI.icons.plus} ${I18N.t('files.new_folder_button')}</button>
                <input type="file" id="pjf-file-input" multiple style="display:none" onchange="AdminPage.pjfUploadFiles(this.files)">
            </div>
            <div id="pjf-breadcrumbs" class="breadcrumbs" style="margin-bottom:8px"></div>
            <div id="pjf-upload-progress" class="upload-progress hidden"></div>
            <div id="pjf-content"><p class="text-muted">${I18N.t('admin.choose_project_hint')}</p></div>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    async onPjfProjectChange() {
        const pId = parseInt(document.getElementById('pjf-project').value);
        if (!pId) {
            document.getElementById('pjf-actions').style.display = 'none';
            document.getElementById('pjf-content').innerHTML = `<p class="text-muted">${I18N.t('admin.choose_project_hint')}</p>`;
            return;
        }
        this._adminProjectFiles.projectId = pId;
        this._adminProjectFiles.folderId = null;
        document.getElementById('pjf-actions').style.display = '';
        await this.loadPjfFiles();
    },

    async loadPjfFiles() {
        const st = this._adminProjectFiles;
        const c = document.getElementById('pjf-content');
        if (!c || !st.projectId) return;
        c.innerHTML = UI.skeletonCards(4);
        try {
            let url = `/api/files?scope=project&project_id=${st.projectId}`;
            if (st.folderId) url += `&folder_id=${st.folderId}`;
            const data = await API._request('GET', url);
            st.files = data.files || [];
            st.folders = data.folders || [];
            st.breadcrumbs = data.breadcrumbs || [];
            this.renderPjfBreadcrumbs();
            this.renderPjfFileList(c);
        } catch (e) { c.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    renderPjfBreadcrumbs() {
        const el = document.getElementById('pjf-breadcrumbs');
        if (!el) return;
        const proj = this._projects.find(p => p.id === this._adminProjectFiles.projectId);
        let h = `<a class="breadcrumb-item" onclick="AdminPage._adminProjectFiles.folderId=null;AdminPage.loadPjfFiles()">${UI.esc(proj ? proj.name : I18N.t('app.project_label'))}</a>`;
        for (const b of this._adminProjectFiles.breadcrumbs) {
            h += `<span class="breadcrumb-sep">/</span><a class="breadcrumb-item" onclick="AdminPage._adminProjectFiles.folderId=${b.id};AdminPage.loadPjfFiles()">${UI.esc(b.name)}</a>`;
        }
        el.innerHTML = h;
    },

    renderPjfFileList(c) {
        const st = this._adminProjectFiles;
        const items = [...st.folders.map(f => ({ ...f, isFolder: true })), ...st.files];
        if (!items.length) { c.innerHTML = `<div class="empty-state"><div class="empty-state-icon">📂</div><p>${I18N.t('files.empty_title')}</p></div>`; return; }
        c.innerHTML = '<div class="file-grid">' + items.map(i => {
            const cls = UI.fileIconClass(i.name, i.isFolder);
            const dbl = i.isFolder ? `AdminPage._adminProjectFiles.folderId=${i.id};AdminPage.loadPjfFiles()` : `UI.openFile(${i.id},${UI.escJson(i.name)},${i.size_bytes || 0})`;
            const ext = i.isFolder ? '' : i.name.split('.').pop().toLowerCase();
            const iconHtml = !i.isFolder && UI.isImage(ext)
                ? `<img class="file-card-thumb" src="/api/files/${i.id}/download" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
                : `<div class="file-card-icon ${cls}">${UI.fileIcon(i.name, i.isFolder)}</div>`;
            return `<div class="file-card" ondblclick="${dbl}" oncontextmenu="AdminPage.showPjfMenu(event,${UI.escJson(i)})">
                ${iconHtml}
                <div class="file-card-name" title="${UI.esc(i.name)}">${UI.esc(i.name)}</div>
                ${!i.isFolder ? `<div class="file-card-meta">${UI.formatBytes(i.size_bytes||0)}</div>` : `<div class="file-card-meta">${I18N.t('files.folder_label')}</div>`}
            </div>`;
        }).join('') + '</div>';
    },

    showPjfMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: I18N.t('files.action_open'), icon: '📂', handler: () => { this._adminProjectFiles.folderId = item.id; this.loadPjfFiles(); } });
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => FilesPage.downloadFolder(item.id, item.name) });
            items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.pjfRenameFolder(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.pjfDeleteFolder(item) });
        } else {
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => FilesPage.download(item.id, item.name) });
            items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.pjfRenameFile(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.pjfDeleteFile(item) });
        }
        UI.showContextMenu(e.clientX, e.clientY, items);
    },

    async pjfUploadFiles(fileList) {
        if (!fileList.length || !this._adminProjectFiles.projectId) return;
        const prog = document.getElementById('pjf-upload-progress');
        prog.classList.remove('hidden');
        const projectId = this._adminProjectFiles.projectId, folderId = this._adminProjectFiles.folderId;
        for (const file of fileList) {
            const id = 'pjfu-' + Math.random().toString(36).substr(2, 6);
            const isLarge = typeof Uploader !== 'undefined' && Uploader.isLarge(file);
            const resumeBadge = isLarge ? `<span class="upload-item-badge" title="${I18N.t('files.upload_resume_hint')}">${I18N.t('files.upload_resume_badge')}</span>` : '';
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)} ${resumeBadge}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            const onProgress = pct => { const f = document.getElementById(id+'-f'), p = document.getElementById(id+'-p'); if (f) f.style.width = pct+'%'; if (p) p.textContent = pct+'%'; };
            try {
                if (isLarge) {
                    await Uploader.uploadLarge(file, 'project', folderId, projectId, onProgress);
                } else {
                    await API.files.upload(file, 'project', folderId, projectId, onProgress);
                }
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(I18N.t('files.upload_item_failed', { name: file.name, error: err.message }), 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadPjfFiles();
        document.getElementById('pjf-file-input').value = '';
    },

    showPjfNewFolder() {
        UI.showModal(I18N.t('files.new_folder_title'), `<div class="form-group"><label>${I18N.t('files.new_folder_name_label')}</label><input type="text" id="pjf-folder-name" class="form-control" placeholder="${I18N.t('files.new_folder_name_placeholder')}"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doPjfCreateFolder()">${I18N.t('common.create')}</button>`);
    },
    async doPjfCreateFolder() {
        const n = document.getElementById('pjf-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, 'project', this._adminProjectFiles.folderId, this._adminProjectFiles.projectId); UI.closeModal(); UI.toast(I18N.t('files.folder_created'), 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    pjfRenameFile(item) {
        UI.showModal(I18N.t('files.rename_file_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="pjf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doPjfRenameFile(${item.id})">${I18N.t('common.rename')}</button>`);
    },
    async doPjfRenameFile(id) { const n = document.getElementById('pjf-rename').value.trim(); if (!n) return; try { await API.files.rename(id, n); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    pjfRenameFolder(item) {
        UI.showModal(I18N.t('files.rename_folder_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="pjf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doPjfRenameFolder(${item.id})">${I18N.t('common.rename')}</button>`);
    },
    async doPjfRenameFolder(id) { const n = document.getElementById('pjf-rename').value.trim(); if (!n) return; try { await API.folders.rename(id, n); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    pjfDeleteFile(item) {
        UI.confirmAction(I18N.t('files.delete_file_title'), I18N.t('files.delete_file_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.files.delete(item.id); UI.toast(I18N.t('admin.deleted'), 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },
    pjfDeleteFolder(item) {
        UI.confirmAction(I18N.t('files.delete_folder_title'), I18N.t('files.delete_folder_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.folders.delete(item.id); UI.toast(I18N.t('admin.deleted'), 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    /* ── Common Files (shared with every employee) ── */
    async renderCommonFiles(el) {
        el.innerHTML = `
        <div class="admin-header"><h2>${I18N.t('admin.nav_common_files')}</h2></div>
        <div style="margin-bottom:12px">
            <button class="btn btn-primary btn-sm" onclick="document.getElementById('cf-file-input').click()">${UI.icons.upload} ${I18N.t('admin.upload_file_button')}</button>
            <button class="btn btn-ghost btn-sm" onclick="AdminPage.showCfNewFolder()">${UI.icons.plus} ${I18N.t('files.new_folder_button')}</button>
            <input type="file" id="cf-file-input" multiple style="display:none" onchange="AdminPage.cfUploadFiles(this.files)">
        </div>
        <div id="cf-breadcrumbs" class="breadcrumbs" style="margin-bottom:8px"></div>
        <div id="cf-upload-progress" class="upload-progress hidden"></div>
        <div id="cf-content">${UI.skeletonCards(4)}</div>`;
        await this.loadCfFiles();
    },

    async loadCfFiles() {
        const st = this._adminCommonFiles;
        const c = document.getElementById('cf-content');
        if (!c) return;
        c.innerHTML = UI.skeletonCards(4);
        try {
            let url = `/api/files?scope=common`;
            if (st.folderId) url += `&folder_id=${st.folderId}`;
            const data = await API._request('GET', url);
            st.files = data.files || [];
            st.folders = data.folders || [];
            st.breadcrumbs = data.breadcrumbs || [];
            this.renderCfBreadcrumbs();
            this.renderCfFileList(c);
        } catch (e) { c.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    renderCfBreadcrumbs() {
        const el = document.getElementById('cf-breadcrumbs');
        if (!el) return;
        let h = `<a class="breadcrumb-item" onclick="AdminPage._adminCommonFiles.folderId=null;AdminPage.loadCfFiles()">${I18N.t('app.nav_common')}</a>`;
        for (const b of this._adminCommonFiles.breadcrumbs) {
            h += `<span class="breadcrumb-sep">/</span><a class="breadcrumb-item" onclick="AdminPage._adminCommonFiles.folderId=${b.id};AdminPage.loadCfFiles()">${UI.esc(b.name)}</a>`;
        }
        el.innerHTML = h;
    },

    renderCfFileList(c) {
        const st = this._adminCommonFiles;
        const items = [...st.folders.map(f => ({ ...f, isFolder: true })), ...st.files];
        if (!items.length) { c.innerHTML = `<div class="empty-state"><div class="empty-state-icon">📂</div><p>${I18N.t('files.empty_title')}</p></div>`; return; }
        c.innerHTML = '<div class="file-grid">' + items.map(i => {
            const cls = UI.fileIconClass(i.name, i.isFolder);
            const dbl = i.isFolder ? `AdminPage._adminCommonFiles.folderId=${i.id};AdminPage.loadCfFiles()` : `UI.openFile(${i.id},${UI.escJson(i.name)},${i.size_bytes || 0})`;
            const ext = i.isFolder ? '' : i.name.split('.').pop().toLowerCase();
            const iconHtml = !i.isFolder && UI.isImage(ext)
                ? `<img class="file-card-thumb" src="/api/files/${i.id}/download" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
                : `<div class="file-card-icon ${cls}">${UI.fileIcon(i.name, i.isFolder)}</div>`;
            return `<div class="file-card" ondblclick="${dbl}" oncontextmenu="AdminPage.showCfMenu(event,${UI.escJson(i)})">
                ${iconHtml}
                <div class="file-card-name" title="${UI.esc(i.name)}">${UI.esc(i.name)}</div>
                ${!i.isFolder ? `<div class="file-card-meta">${UI.formatBytes(i.size_bytes||0)}</div>` : `<div class="file-card-meta">${I18N.t('files.folder_label')}</div>`}
            </div>`;
        }).join('') + '</div>';
    },

    showCfMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: I18N.t('files.action_open'), icon: '📂', handler: () => { this._adminCommonFiles.folderId = item.id; this.loadCfFiles(); } });
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => FilesPage.downloadFolder(item.id, item.name) });
            items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.cfRenameFolder(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.cfDeleteFolder(item) });
        } else {
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => FilesPage.download(item.id, item.name) });
            items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.cfRenameFile(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.cfDeleteFile(item) });
        }
        UI.showContextMenu(e.clientX, e.clientY, items);
    },

    async cfUploadFiles(fileList) {
        if (!fileList.length) return;
        const prog = document.getElementById('cf-upload-progress');
        prog.classList.remove('hidden');
        const folderId = this._adminCommonFiles.folderId;
        for (const file of fileList) {
            const id = 'cfu-' + Math.random().toString(36).substr(2, 6);
            const isLarge = typeof Uploader !== 'undefined' && Uploader.isLarge(file);
            const resumeBadge = isLarge ? `<span class="upload-item-badge" title="${I18N.t('files.upload_resume_hint')}">${I18N.t('files.upload_resume_badge')}</span>` : '';
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)} ${resumeBadge}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            const onProgress = pct => { const f = document.getElementById(id+'-f'), p = document.getElementById(id+'-p'); if (f) f.style.width = pct+'%'; if (p) p.textContent = pct+'%'; };
            try {
                if (isLarge) {
                    await Uploader.uploadLarge(file, 'common', folderId, null, onProgress);
                } else {
                    await API.files.upload(file, 'common', folderId, null, onProgress);
                }
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(I18N.t('files.upload_item_failed', { name: file.name, error: err.message }), 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadCfFiles();
        document.getElementById('cf-file-input').value = '';
    },

    showCfNewFolder() {
        UI.showModal(I18N.t('files.new_folder_title'), `<div class="form-group"><label>${I18N.t('files.new_folder_name_label')}</label><input type="text" id="cf-folder-name" class="form-control" placeholder="${I18N.t('files.new_folder_name_placeholder')}"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doCfCreateFolder()">${I18N.t('common.create')}</button>`);
    },
    async doCfCreateFolder() {
        const n = document.getElementById('cf-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, 'common', this._adminCommonFiles.folderId); UI.closeModal(); UI.toast(I18N.t('files.folder_created'), 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    cfRenameFile(item) {
        UI.showModal(I18N.t('files.rename_file_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="cf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doCfRenameFile(${item.id})">${I18N.t('common.rename')}</button>`);
    },
    async doCfRenameFile(id) { const n = document.getElementById('cf-rename').value.trim(); if (!n) return; try { await API.files.rename(id, n); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    cfRenameFolder(item) {
        UI.showModal(I18N.t('files.rename_folder_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="cf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="AdminPage.doCfRenameFolder(${item.id})">${I18N.t('common.rename')}</button>`);
    },
    async doCfRenameFolder(id) { const n = document.getElementById('cf-rename').value.trim(); if (!n) return; try { await API.folders.rename(id, n); UI.closeModal(); UI.toast(I18N.t('admin.updated'), 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    cfDeleteFile(item) {
        UI.confirmAction(I18N.t('files.delete_file_title'), I18N.t('files.delete_file_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.files.delete(item.id); UI.toast(I18N.t('admin.deleted'), 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
        });
    },
    cfDeleteFolder(item) {
        UI.confirmAction(I18N.t('files.delete_folder_title'), I18N.t('files.delete_folder_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.folders.delete(item.id); UI.toast(I18N.t('admin.deleted'), 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
        });
    }
};
