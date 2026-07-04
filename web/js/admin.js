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
                <div class="admin-title">${UI.icons.settings} Dolandyryş</div>
                <nav class="admin-nav">
                    <a class="admin-nav-item ${this.currentTab === 'dashboard' ? 'active' : ''}" onclick="AdminPage.switchTab('dashboard')">${UI.icons.dashboard} Statistika</a>
                    <a class="admin-nav-item ${this.currentTab === 'projects' ? 'active' : ''}" onclick="AdminPage.switchTab('projects')">${UI.icons.users} Taslamalar</a>
                    <a class="admin-nav-item ${this.currentTab === 'users' ? 'active' : ''}" onclick="AdminPage.switchTab('users')">${UI.icons.user} Işgärler</a>
                    <a class="admin-nav-item ${this.currentTab === 'project-files' ? 'active' : ''}" onclick="AdminPage.switchTab('project-files')">📁 Taslama faýllary</a>
                    <a class="admin-nav-item ${this.currentTab === 'common-files' ? 'active' : ''}" onclick="AdminPage.switchTab('common-files')">🌐 Umumy faýllar</a>
                </nav>
            </div>
            <div class="admin-content" id="admin-content"></div>
        </div>`;
    },

    async init() { await this.switchTab(this.currentTab); },

    async switchTab(tab) {
        this.currentTab = tab;
        document.querySelectorAll('.admin-nav-item').forEach((el, i) => {
            el.classList.toggle('active', ['dashboard','projects','users','project-files','common-files'][i] === tab);
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
        }
    },

    /* ── Dashboard ── */
    async renderDashboard(el) {
        try {
            const [d, pq] = await Promise.all([API.admin.dashboard(), API.admin.publicQuota.get()]);
            const pqMB = Math.round((pq.quota_bytes || 53687091200) / (1024 * 1024));
            el.innerHTML = `
            <h2 style="font-size:1.1rem;font-weight:600;margin-bottom:16px">Statistika</h2>
            <div class="stat-cards">
                <div class="stat-card"><div class="stat-card-value">${d.total_users || 0}</div><div class="stat-card-label">Işgärler</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_projects || 0}</div><div class="stat-card-label">Taslamalar</div></div>
                <div class="stat-card"><div class="stat-card-value">${d.total_files || 0}</div><div class="stat-card-label">Faýllar</div></div>
                <div class="stat-card"><div class="stat-card-value">${UI.formatBytes(d.total_bytes || 0)}</div><div class="stat-card-label">Ulanylýan ýer</div></div>
            </div>
            <h3 style="font-size:1rem;font-weight:600;margin:24px 0 12px">Umumy ammar kwotasy</h3>
            <div style="display:flex;align-items:center;gap:10px">
                <input type="number" id="public-quota-mb" class="form-control" value="${pqMB}" min="1" style="width:160px">
                <span class="text-muted" style="font-size:.82rem">MB</span>
                <button class="btn btn-primary btn-sm" onclick="AdminPage.savePublicQuota()">Ýatda sakla</button>
            </div>
            <p class="text-muted" style="font-size:.78rem;margin-top:6px">Ähli işgärlere açyk "Umumy" papka üçin umumy kwota.</p>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    async savePublicQuota() {
        const mb = parseInt(document.getElementById('public-quota-mb').value) || 0;
        if (mb <= 0) { UI.toast('Dogry kwota giriziň', 'error'); return; }
        try { await API.admin.publicQuota.set(mb); UI.toast('Umumy kwota üýtgedildi', 'success'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Projects ── */
    async renderProjects(el) {
        try {
            const items = (await API.admin.projects.list()) || [];
            this._projects = items;
            el.innerHTML = `
            <div class="admin-header"><h2>Taslamalar</h2><div style="display:flex;gap:8px">
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.showBulkProjectQuota()">📊 Kwota hemmesine</button>
                <button class="btn btn-primary btn-sm" onclick="AdminPage.showProjectModal()">${UI.icons.plus} Täze taslama</button>
            </div></div>
            <p class="text-muted" style="font-size:.82rem;margin-bottom:12px">Her taslama — aýratyn papka. Diňe siziň goşan işgärleriňiz ol papka girip bilýär.</p>
            <table class="admin-table"><thead><tr><th>ID</th><th>Ady</th><th>Kwota</th><th>Hereketler</th></tr></thead><tbody>
            ${items.map(p => `<tr><td>${p.id}</td><td>${UI.esc(p.name)}</td><td>${UI.formatBytes(p.quota_bytes || 0)}</td><td>
                <button class="btn btn-sm btn-ghost" onclick="AdminPage.showMembersModal(${p.id},'${UI.esc(p.name)}')">👥 Gatnaşyjylar</button>
                <button class="btn btn-sm btn-ghost" onclick="AdminPage.showProjectModal(${p.id},'${UI.esc(p.name)}',${p.quota_bytes||0})">✏️</button>
                <button class="btn btn-sm btn-danger" onclick="AdminPage.deleteProject(${p.id})">🗑</button></td></tr>`).join('')}
            ${!items.length ? '<tr><td colspan="4" class="text-muted text-center">Taslama ýok</td></tr>' : ''}
            </tbody></table>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    showProjectModal(id, name, quotaBytes) {
        const edit = !!id;
        const quotaMB = Math.round((quotaBytes || 5368709120) / (1024 * 1024));
        UI.showModal(edit ? 'Taslamany üýtget' : 'Täze taslama',
            `<div class="form-group"><label>Ady</label><input type="text" id="proj-name" value="${name||''}" class="form-control" placeholder="Taslamanyň ady"></div>
             <div class="form-group"><label>Kwota (MB)</label><input type="number" id="proj-quota" value="${quotaMB}" class="form-control" min="1"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.saveProject(${id||'null'})">${edit ? 'Üýtget' : 'Döret'}</button>`);
    },
    async saveProject(id) {
        const n = document.getElementById('proj-name').value.trim();
        const quotaMB = parseInt(document.getElementById('proj-quota').value) || 5120;
        const quotaBytes = quotaMB * 1024 * 1024;
        if (!n) { UI.toast('Ady giriziň', 'error'); return; }
        try {
            if (id) await API.admin.projects.update(id, n, quotaBytes); else await API.admin.projects.create(n, quotaBytes);
            UI.closeModal(); UI.toast(id ? 'Üýtgedildi' : 'Döredildi', 'success'); this.switchTab('projects');
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    async deleteProject(id) {
        if (!confirm('Bu taslamany we onuň ähli faýllaryny pozmak isleýärsiňizmi?')) return;
        try { await API.admin.projects.delete(id); UI.toast('Pozuldy', 'success'); this.switchTab('projects'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    showBulkProjectQuota() {
        UI.showModal('Ähli taslamalaryň kwotasy', `
            <div class="form-group"><label>Täze kwota (MB)</label><input type="number" id="bulk-project-quota" class="form-control" value="5120" min="1"></div>
            <p class="text-muted" style="font-size:.78rem">Bu ähli taslamalaryň kwotasyny üýtgeder.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-primary" onclick="AdminPage.doBulkProjectQuota()">Üýtget</button>`);
    },
    async doBulkProjectQuota() {
        const mb = parseInt(document.getElementById('bulk-project-quota').value) || 0;
        if (mb <= 0) { UI.toast('Dogry kwota giriziň', 'error'); return; }
        try { await API.admin.projects.bulkQuota(mb); UI.closeModal(); UI.toast('Ähli taslamalaryň kwotasy üýtgedildi', 'success'); this.switchTab('projects'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Project members (ACL) ── */
    async showMembersModal(projectId, projectName) {
        UI.showModal(`Gatnaşyjylar: ${UI.esc(projectName)}`, `
            <div class="form-group">
                <label>Işgär goşmak</label>
                <div style="display:flex;gap:6px">
                    <input type="text" id="member-search" class="form-control" placeholder="Ady ýa-da ulanyjy ady…" oninput="AdminPage.searchMemberCandidates(${projectId})">
                    <select id="member-permission" class="form-control" style="width:140px">
                        <option value="view">Diňe görmek</option>
                        <option value="edit">Redaktirlemek</option>
                    </select>
                </div>
                <div id="member-search-results" class="member-search-results"></div>
            </div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <div id="member-list"><div class="spinner"></div></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýapmak</button>`);
        this._loadMembers(projectId);
    },

    async _loadMembers(projectId) {
        const el = document.getElementById('member-list');
        if (!el) return;
        try {
            const members = (await API.admin.projects.members.list(projectId)) || [];
            if (!members.length) { el.innerHTML = '<p class="text-muted" style="font-size:.82rem">Heniz gatnaşyjy ýok</p>'; return; }
            el.innerHTML = members.map(m => `
                <div class="member-row" style="display:flex;align-items:center;justify-content:space-between;padding:6px 0">
                    <div><strong>${UI.esc(m.full_name || m.username)}</strong> <span class="text-muted">@${UI.esc(m.username)}</span></div>
                    <div style="display:flex;gap:6px;align-items:center">
                        <select class="form-control" style="width:150px" onchange="AdminPage.changeMemberPermission(${projectId},${m.user_id},this.value)">
                            <option value="view" ${m.permission==='view'?'selected':''}>Diňe görmek</option>
                            <option value="edit" ${m.permission==='edit'?'selected':''}>Redaktirlemek</option>
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
                    </div>`).join('') || '<div class="text-muted" style="font-size:.8rem;padding:4px 0">Tapylmady</div>';
            } catch { resEl.innerHTML = ''; }
        }, 250);
    },

    async addMember(projectId, userId) {
        const permission = document.getElementById('member-permission').value;
        try {
            await API.admin.projects.members.add(projectId, userId, permission);
            document.getElementById('member-search').value = '';
            document.getElementById('member-search-results').innerHTML = '';
            UI.toast('Goşuldy', 'success');
            this._loadMembers(projectId);
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    async changeMemberPermission(projectId, userId, permission) {
        try { await API.admin.projects.members.update(projectId, userId, permission); UI.toast('Üýtgedildi', 'success'); } catch (e) { UI.toast(e.message, 'error'); }
    },
    async removeMember(projectId, userId) {
        try { await API.admin.projects.members.remove(projectId, userId); UI.toast('Aýryldy', 'success'); this._loadMembers(projectId); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Users ── */
    async renderUsers(el) {
        try {
            const users = (await API.admin.users.list()) || [];
            el.innerHTML = `
            <div class="admin-header"><h2>Işgärler</h2>
                <div style="display:flex;gap:8px;align-items:center">
                    <input type="text" id="admin-user-search" class="form-control" placeholder="Gözle…" style="width:200px" oninput="AdminPage.filterUsers(this.value)">
                    <button class="btn btn-ghost btn-sm" onclick="AdminPage.showBulkUserQuota()">📊 Kwota hemmesine</button>
                    <button class="btn btn-danger btn-sm" onclick="AdminPage.confirmDeleteAllUsers()">🗑 Hemmesini poz</button>
                    <button class="btn btn-ghost btn-sm" onclick="AdminPage.showImportModal()">📥 Import</button>
                    <button class="btn btn-primary btn-sm" onclick="AdminPage.showCreateUserModal()">${UI.icons.plus} Täze</button>
                </div>
            </div>
            <table class="admin-table" id="admin-users-table"><thead><tr><th>ID</th><th>Ady</th><th>Ulanyjy ady</th><th>Rol</th><th>Kwota</th><th>Hereketler</th></tr></thead><tbody>
            ${users.map(u => `<tr data-uid="${u.id}"><td>${u.id}</td><td>${UI.esc(u.full_name)}</td><td>@${UI.esc(u.username)}</td>
                <td><span class="badge badge-${u.role === 'admin' ? 'admin' : 'user'}">${u.role === 'admin' ? 'Admin' : 'Işgär'}</span></td>
                <td>${UI.formatBytes(u.quota_bytes || 0)}</td>
                <td><button class="btn btn-sm btn-ghost" onclick="AdminPage.showEditUserModal(${u.id})">✏️</button>
                ${u.role !== 'admin' ? `<button class="btn btn-sm btn-danger" onclick="AdminPage.deleteUser(${u.id})">🗑</button>` : ''}</td></tr>`).join('')}
            ${!users.length ? '<tr><td colspan="6" class="text-muted text-center">Işgär ýok</td></tr>' : ''}
            </tbody></table>`;
            this._users = users;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    filterUsers(q) {
        const lc = q.toLowerCase();
        document.querySelectorAll('#admin-users-table tbody tr').forEach(r => { r.style.display = r.textContent.toLowerCase().includes(lc) ? '' : 'none'; });
    },

    showCreateUserModal() {
        UI.showModal('Täze işgär', `
            <div class="form-group"><label>Doly ady</label><input type="text" id="nu-name" class="form-control" placeholder="Ady we familiýasy"></div>
            <div class="form-group"><label>Ulanyjy ady</label><input type="text" id="nu-username" class="form-control" placeholder="username"></div>
            <div class="form-group"><label>Parol</label>${UI.passwordField('nu-password', 'Azyndan 6 simwol')}</div>
            <div class="form-group"><label>Rol</label><select id="nu-role" class="form-control"><option value="user">Işgär</option><option value="admin">Admin</option></select></div>
            <div class="form-group"><label>Kwota (MB)</label><input type="number" id="nu-quota" class="form-control" value="10240" min="0"></div>
            <p class="text-muted" style="font-size:.78rem">Taslamalara goşmak üçin "Taslamalar" bölüminden gatnaşyjy hökmünde goşuň.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-primary" onclick="AdminPage.doCreateUser()">Döret</button>`);
    },

    async doCreateUser() {
        const name = document.getElementById('nu-name').value.trim();
        const username = document.getElementById('nu-username').value.trim();
        const password = document.getElementById('nu-password').value;
        const role = document.getElementById('nu-role').value;
        const quotaMB = parseInt(document.getElementById('nu-quota').value) || 0;
        if (!name || !username || !password) { UI.toast('Ähli meýdanlary dolduryň', 'error'); return; }
        try {
            await API.admin.users.create({ full_name: name, username, password, role, quota_mb: quotaMB });
            UI.closeModal(); UI.toast('Işgär döredildi', 'success'); this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    showEditUserModal(id) {
        const u = this._users.find(x => x.id === id); if (!u) return;
        const mb = Math.round((u.quota_bytes || 0) / (1024 * 1024));
        UI.showModal('Işgäri üýtget', `
            <div class="form-group"><label>Doly ady</label><input type="text" id="eu-name" value="${UI.esc(u.full_name)}" class="form-control"></div>
            <div class="form-group"><label>Täze parol</label>${UI.passwordField('eu-password', 'Boş goýsaň üýtgemez')}</div>
            <div class="form-group"><label>Rol</label><select id="eu-role" class="form-control"><option value="user" ${u.role==='user'?'selected':''}>Işgär</option><option value="admin" ${u.role==='admin'?'selected':''}>Admin</option></select></div>
            <div class="form-group"><label>Kwota (MB)</label><input type="number" id="eu-quota" value="${mb}" class="form-control" min="0"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-primary" onclick="AdminPage.saveUser(${id})">Ýatda sakla</button>`);
    },
    async saveUser(id) {
        const role = document.getElementById('eu-role').value;
        const mb = parseInt(document.getElementById('eu-quota').value) || 0;
        const name = document.getElementById('eu-name').value.trim();
        const password = document.getElementById('eu-password').value;
        const data = { role, quota_bytes: mb * 1024 * 1024 };
        if (name) data.display_name = name;
        if (password) data.password = password;
        try { await API.admin.users.update(id, data); UI.closeModal(); UI.toast('Üýtgedildi', 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
    },
    async deleteUser(id) {
        if (!confirm('Bu işgäri pozmak isleýärsiňizmi?')) return;
        try { await API.admin.users.delete(id); UI.toast('Pozuldy', 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    confirmDeleteAllUsers() {
        UI.showModal('Ähli işgärleri pozmak', `
            <p style="color:var(--danger);font-weight:600">⚠️ Bu ähli işgärleri (admin-den başga) pozar!</p>
            <p class="text-muted" style="font-size:.85rem">Bu hereket yzyna gaýtaryp bolmaz. Tassyklamak üçin aşakda "POZMAK" ýazyň.</p>
            <div class="form-group"><input type="text" id="confirm-delete-all" class="form-control" placeholder='POZMAK ýazyň'></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-danger" onclick="AdminPage.doDeleteAllUsers()">Hemmesini poz</button>`);
    },
    async doDeleteAllUsers() {
        if (document.getElementById('confirm-delete-all').value.trim() !== 'POZMAK') { UI.toast('Tassyklamak üçin "POZMAK" ýazyň', 'error'); return; }
        try { const res = await API.admin.users.deleteAll(); UI.closeModal(); UI.toast(`${res.deleted} işgär pozuldy`, 'success'); this.switchTab('users'); }
        catch (e) { UI.toast(e.message, 'error'); }
    },

    showBulkUserQuota() {
        UI.showModal('Ähli işgärleriň kwotasy', `
            <div class="form-group"><label>Täze kwota (MB)</label><input type="number" id="bulk-user-quota" class="form-control" value="10240" min="1"></div>
            <p class="text-muted" style="font-size:.78rem">Bu ähli işgärleriň (admin-den başga) kwotasyny üýtgeder.</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-primary" onclick="AdminPage.doBulkUserQuota()">Üýtget</button>`);
    },
    async doBulkUserQuota() {
        const mb = parseInt(document.getElementById('bulk-user-quota').value) || 0;
        if (mb <= 0) { UI.toast('Dogry kwota giriziň', 'error'); return; }
        try { await API.admin.users.bulkQuota(mb); UI.closeModal(); UI.toast('Ähli işgärleriň kwotasy üýtgedildi', 'success'); this.switchTab('users'); } catch (e) { UI.toast(e.message, 'error'); }
    },

    showImportModal() {
        UI.showModal('Işgärleri import etmek', `
            <p class="text-muted" style="font-size:.82rem;margin-bottom:12px">CSV ýa-da XLSX faýly ýükläň. Faýl formaty:<br>
            <code style="font-size:.75rem">username, password, full_name, quota_mb</code></p>
            <div class="form-group">
                <input type="file" id="import-file" class="form-control" accept=".csv,.xlsx,.xls">
            </div>
            <div id="import-results" style="display:none;max-height:200px;overflow:auto;margin-top:8px"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyr</button><button class="btn btn-primary" id="import-btn" onclick="AdminPage.doImportUsers()">Import et</button>`);
    },

    async doImportUsers() {
        const fileInput = document.getElementById('import-file');
        const file = fileInput?.files[0];
        if (!file) { UI.toast('Faýl saýlaň', 'error'); return; }
        const btn = document.getElementById('import-btn');
        btn.disabled = true; btn.textContent = 'Ýüklenýär…';
        try {
            const result = await API.admin.users.importFile(file);
            const el = document.getElementById('import-results');
            el.style.display = 'block';
            let html = `<p style="font-weight:600;margin-bottom:6px">Netije: ${result.created}/${result.total} döredildi</p>`;
            if (result.results) {
                html += '<div style="font-size:.78rem">';
                result.results.forEach(r => {
                    html += `<div style="padding:2px 0;color:${r.success ? 'var(--success)' : 'var(--danger)'}">${UI.esc(r.username)}: ${r.success ? '✓ döredildi' : '✕ ' + UI.esc(r.error)}</div>`;
                });
                html += '</div>';
            }
            el.innerHTML = html;
            if (result.created > 0) this.switchTab('users');
        } catch (e) { UI.toast(e.message, 'error'); }
        finally { btn.disabled = false; btn.textContent = 'Import et'; }
    },

    /* ── Project Files (admin browses any project's storage) ── */
    async renderProjectFiles(el) {
        try {
            const projects = (await API.admin.projects.list()) || [];
            this._projects = projects;
            const st = this._adminProjectFiles;
            const opts = projects.map(p => `<option value="${p.id}">${UI.esc(p.name)}</option>`).join('');
            el.innerHTML = `
            <div class="admin-header"><h2>Taslama faýllary</h2></div>
            <div style="display:flex;gap:10px;margin-bottom:16px;flex-wrap:wrap;align-items:end">
                <div class="form-group" style="margin:0"><label style="font-size:.78rem">Taslama</label><select id="pjf-project" class="form-control" style="width:220px" onchange="AdminPage.onPjfProjectChange()"><option value="">Saýlaň…</option>${opts}</select></div>
            </div>
            <div id="pjf-actions" style="display:none;margin-bottom:12px">
                <button class="btn btn-primary btn-sm" onclick="document.getElementById('pjf-file-input').click()">${UI.icons.upload} Faýl ýükle</button>
                <button class="btn btn-ghost btn-sm" onclick="AdminPage.showPjfNewFolder()">${UI.icons.plus} Täze papka</button>
                <input type="file" id="pjf-file-input" multiple style="display:none" onchange="AdminPage.pjfUploadFiles(this.files)">
            </div>
            <div id="pjf-breadcrumbs" class="breadcrumbs" style="margin-bottom:8px"></div>
            <div id="pjf-upload-progress" class="upload-progress hidden"></div>
            <div id="pjf-content"><p class="text-muted">Taslamany saýlaň</p></div>`;
        } catch (e) { el.innerHTML = `<p class="text-muted">${UI.esc(e.message)}</p>`; }
    },

    async onPjfProjectChange() {
        const pId = parseInt(document.getElementById('pjf-project').value);
        if (!pId) {
            document.getElementById('pjf-actions').style.display = 'none';
            document.getElementById('pjf-content').innerHTML = '<p class="text-muted">Taslamany saýlaň</p>';
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
        let h = `<a class="breadcrumb-item" onclick="AdminPage._adminProjectFiles.folderId=null;AdminPage.loadPjfFiles()">${UI.esc(proj ? proj.name : 'Taslama')}</a>`;
        for (const b of this._adminProjectFiles.breadcrumbs) {
            h += `<span class="breadcrumb-sep">/</span><a class="breadcrumb-item" onclick="AdminPage._adminProjectFiles.folderId=${b.id};AdminPage.loadPjfFiles()">${UI.esc(b.name)}</a>`;
        }
        el.innerHTML = h;
    },

    renderPjfFileList(c) {
        const st = this._adminProjectFiles;
        const items = [...st.folders.map(f => ({ ...f, isFolder: true })), ...st.files];
        if (!items.length) { c.innerHTML = '<div class="empty-state"><div class="empty-state-icon">📂</div><p>Bu ýerde faýl ýok</p></div>'; return; }
        c.innerHTML = '<div class="file-grid">' + items.map(i => {
            const icon = UI.fileIcon(i.name, i.isFolder);
            const cls = UI.fileIconClass(i.name, i.isFolder);
            let dbl = i.isFolder ? `AdminPage._adminProjectFiles.folderId=${i.id};AdminPage.loadPjfFiles()` : `FilesPage.download(${i.id},'${UI.esc(i.name)}')`;
            if (!i.isFolder && UI.isMediaPreviewable(i.name)) dbl = `PreviewPage.open(${i.id},'${UI.esc(i.name)}')`;
            else if (!i.isFolder && UI.isCollaboraViewable(i.name)) dbl = `EditorPage.open(${i.id},'${UI.esc(i.name)}')`;
            return `<div class="file-card" ondblclick="${dbl}" oncontextmenu="AdminPage.showPjfMenu(event,${JSON.stringify(i).replace(/"/g,'&quot;')})">
                <div class="file-card-icon ${cls}">${icon}</div>
                <div class="file-card-name" title="${UI.esc(i.name)}">${UI.esc(i.name)}</div>
                ${!i.isFolder ? `<div class="file-card-meta">${UI.formatBytes(i.size_bytes||0)}</div>` : '<div class="file-card-meta">Papka</div>'}
            </div>`;
        }).join('') + '</div>';
    },

    showPjfMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: 'Aç', icon: '📂', handler: () => { this._adminProjectFiles.folderId = item.id; this.loadPjfFiles(); } });
            items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.pjfRenameFolder(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.pjfDeleteFolder(item) });
        } else {
            items.push({ action: 'download', label: 'Ýükle', icon: '📥', handler: () => FilesPage.download(item.id, item.name) });
            items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.pjfRenameFile(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.pjfDeleteFile(item) });
        }
        UI.showContextMenu(e.clientX, e.clientY, items);
    },

    async pjfUploadFiles(fileList) {
        if (!fileList.length || !this._adminProjectFiles.projectId) return;
        const prog = document.getElementById('pjf-upload-progress');
        prog.classList.remove('hidden');
        for (const file of fileList) {
            const id = 'pjfu-' + Math.random().toString(36).substr(2, 6);
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            try {
                const form = new FormData();
                form.append('file', file);
                form.append('scope', 'project');
                form.append('project_id', String(this._adminProjectFiles.projectId));
                if (this._adminProjectFiles.folderId) form.append('folder_id', String(this._adminProjectFiles.folderId));
                await new Promise((resolve, reject) => {
                    const xhr = new XMLHttpRequest();
                    xhr.open('POST', '/api/files/upload');
                    xhr.withCredentials = true;
                    xhr.upload.onprogress = (ev) => { if (ev.lengthComputable) { const pct = Math.round(ev.loaded / ev.total * 100); const f = document.getElementById(id+'-f'), p = document.getElementById(id+'-p'); if (f) f.style.width = pct+'%'; if (p) p.textContent = pct+'%'; } };
                    xhr.onload = () => { if (xhr.status >= 200 && xhr.status < 300) resolve(); else { try { reject(new Error(JSON.parse(xhr.responseText).error)); } catch { reject(new Error('Ýükläp bolmady')); } } };
                    xhr.onerror = () => reject(new Error('Tor näsazlygy'));
                    xhr.send(form);
                });
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(`"${file.name}": ${err.message}`, 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadPjfFiles();
        document.getElementById('pjf-file-input').value = '';
    },

    showPjfNewFolder() {
        UI.showModal('Täze papka', `<div class="form-group"><label>Papkanyň ady</label><input type="text" id="pjf-folder-name" class="form-control" placeholder="Papka ady"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doPjfCreateFolder()">Döret</button>`);
    },
    async doPjfCreateFolder() {
        const n = document.getElementById('pjf-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, 'project', this._adminProjectFiles.folderId, this._adminProjectFiles.projectId); UI.closeModal(); UI.toast('Papka döredildi', 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    pjfRenameFile(item) {
        UI.showModal('Adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="pjf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doPjfRenameFile(${item.id})">Üýtget</button>`);
    },
    async doPjfRenameFile(id) { const n = document.getElementById('pjf-rename').value.trim(); if (!n) return; try { await API.files.rename(id, n); UI.closeModal(); UI.toast('Üýtgedildi', 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    pjfRenameFolder(item) {
        UI.showModal('Papkanyň adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="pjf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doPjfRenameFolder(${item.id})">Üýtget</button>`);
    },
    async doPjfRenameFolder(id) { const n = document.getElementById('pjf-rename').value.trim(); if (!n) return; try { await API.folders.rename(id, n); UI.closeModal(); UI.toast('Üýtgedildi', 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    async pjfDeleteFile(item) {
        if (!confirm(`"${item.name}" faýlyny pozmak isleýärsiňizmi?`)) return;
        try { await API.files.delete(item.id); UI.toast('Pozuldy', 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    async pjfDeleteFolder(item) {
        if (!confirm(`"${item.name}" papkasyny pozmak isleýärsiňizmi?`)) return;
        try { await API.folders.delete(item.id); UI.toast('Pozuldy', 'success'); this.loadPjfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Common Files (shared with every employee) ── */
    async renderCommonFiles(el) {
        el.innerHTML = `
        <div class="admin-header"><h2>Umumy faýllar</h2></div>
        <div style="margin-bottom:12px">
            <button class="btn btn-primary btn-sm" onclick="document.getElementById('cf-file-input').click()">${UI.icons.upload} Faýl ýükle</button>
            <button class="btn btn-ghost btn-sm" onclick="AdminPage.showCfNewFolder()">${UI.icons.plus} Täze papka</button>
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
        let h = `<a class="breadcrumb-item" onclick="AdminPage._adminCommonFiles.folderId=null;AdminPage.loadCfFiles()">Umumy</a>`;
        for (const b of this._adminCommonFiles.breadcrumbs) {
            h += `<span class="breadcrumb-sep">/</span><a class="breadcrumb-item" onclick="AdminPage._adminCommonFiles.folderId=${b.id};AdminPage.loadCfFiles()">${UI.esc(b.name)}</a>`;
        }
        el.innerHTML = h;
    },

    renderCfFileList(c) {
        const st = this._adminCommonFiles;
        const items = [...st.folders.map(f => ({ ...f, isFolder: true })), ...st.files];
        if (!items.length) { c.innerHTML = '<div class="empty-state"><div class="empty-state-icon">📂</div><p>Bu ýerde faýl ýok</p></div>'; return; }
        c.innerHTML = '<div class="file-grid">' + items.map(i => {
            const icon = UI.fileIcon(i.name, i.isFolder);
            const cls = UI.fileIconClass(i.name, i.isFolder);
            let dbl = i.isFolder ? `AdminPage._adminCommonFiles.folderId=${i.id};AdminPage.loadCfFiles()` : `FilesPage.download(${i.id},'${UI.esc(i.name)}')`;
            if (!i.isFolder && UI.isMediaPreviewable(i.name)) dbl = `PreviewPage.open(${i.id},'${UI.esc(i.name)}')`;
            else if (!i.isFolder && UI.isCollaboraViewable(i.name)) dbl = `EditorPage.open(${i.id},'${UI.esc(i.name)}')`;
            return `<div class="file-card" ondblclick="${dbl}" oncontextmenu="AdminPage.showCfMenu(event,${JSON.stringify(i).replace(/"/g,'&quot;')})">
                <div class="file-card-icon ${cls}">${icon}</div>
                <div class="file-card-name" title="${UI.esc(i.name)}">${UI.esc(i.name)}</div>
                ${!i.isFolder ? `<div class="file-card-meta">${UI.formatBytes(i.size_bytes||0)}</div>` : '<div class="file-card-meta">Papka</div>'}
            </div>`;
        }).join('') + '</div>';
    },

    showCfMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: 'Aç', icon: '📂', handler: () => { this._adminCommonFiles.folderId = item.id; this.loadCfFiles(); } });
            items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.cfRenameFolder(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.cfDeleteFolder(item) });
        } else {
            items.push({ action: 'download', label: 'Ýükle', icon: '📥', handler: () => FilesPage.download(item.id, item.name) });
            items.push({ action: 'rename', label: 'Adyny üýtget', icon: '✏️', handler: () => this.cfRenameFile(item) });
            items.push({ divider: true });
            items.push({ action: 'delete', label: 'Poz', icon: '🗑', danger: true, handler: () => this.cfDeleteFile(item) });
        }
        UI.showContextMenu(e.clientX, e.clientY, items);
    },

    async cfUploadFiles(fileList) {
        if (!fileList.length) return;
        const prog = document.getElementById('cf-upload-progress');
        prog.classList.remove('hidden');
        for (const file of fileList) {
            const id = 'cfu-' + Math.random().toString(36).substr(2, 6);
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            try {
                const form = new FormData();
                form.append('file', file);
                form.append('scope', 'common');
                if (this._adminCommonFiles.folderId) form.append('folder_id', String(this._adminCommonFiles.folderId));
                await new Promise((resolve, reject) => {
                    const xhr = new XMLHttpRequest();
                    xhr.open('POST', '/api/files/upload');
                    xhr.withCredentials = true;
                    xhr.upload.onprogress = (ev) => { if (ev.lengthComputable) { const pct = Math.round(ev.loaded / ev.total * 100); const f = document.getElementById(id+'-f'), p = document.getElementById(id+'-p'); if (f) f.style.width = pct+'%'; if (p) p.textContent = pct+'%'; } };
                    xhr.onload = () => { if (xhr.status >= 200 && xhr.status < 300) resolve(); else { try { reject(new Error(JSON.parse(xhr.responseText).error)); } catch { reject(new Error('Ýükläp bolmady')); } } };
                    xhr.onerror = () => reject(new Error('Tor näsazlygy'));
                    xhr.send(form);
                });
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(`"${file.name}": ${err.message}`, 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadCfFiles();
        document.getElementById('cf-file-input').value = '';
    },

    showCfNewFolder() {
        UI.showModal('Täze papka', `<div class="form-group"><label>Papkanyň ady</label><input type="text" id="cf-folder-name" class="form-control" placeholder="Papka ady"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doCfCreateFolder()">Döret</button>`);
    },
    async doCfCreateFolder() {
        const n = document.getElementById('cf-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, 'common', this._adminCommonFiles.folderId); UI.closeModal(); UI.toast('Papka döredildi', 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    cfRenameFile(item) {
        UI.showModal('Adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="cf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doCfRenameFile(${item.id})">Üýtget</button>`);
    },
    async doCfRenameFile(id) { const n = document.getElementById('cf-rename').value.trim(); if (!n) return; try { await API.files.rename(id, n); UI.closeModal(); UI.toast('Üýtgedildi', 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    cfRenameFolder(item) {
        UI.showModal('Papkanyň adyny üýtget', `<div class="form-group"><label>Täze ady</label><input type="text" id="cf-rename" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">Ýatyrmak</button><button class="btn btn-primary" onclick="AdminPage.doCfRenameFolder(${item.id})">Üýtget</button>`);
    },
    async doCfRenameFolder(id) { const n = document.getElementById('cf-rename').value.trim(); if (!n) return; try { await API.folders.rename(id, n); UI.closeModal(); UI.toast('Üýtgedildi', 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); } },
    async cfDeleteFile(item) {
        if (!confirm(`"${item.name}" faýlyny pozmak isleýärsiňizmi?`)) return;
        try { await API.files.delete(item.id); UI.toast('Pozuldy', 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },
    async cfDeleteFolder(item) {
        if (!confirm(`"${item.name}" papkasyny pozmak isleýärsiňizmi?`)) return;
        try { await API.folders.delete(item.id); UI.toast('Pozuldy', 'success'); this.loadCfFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    }
};
