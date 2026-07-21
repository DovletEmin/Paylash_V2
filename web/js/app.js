/* Paylash — Main App Router */
const App = {
    user: null,
    currentPage: null,
    projects: [],
    config: { allow_registration: true },

    async start() {
        this.initTheme();
        await this.checkAuth();
        try { this.config = await API.public.config(); } catch {}
        window.addEventListener('popstate', () => this.route());
        this.route();
        this.checkForcedPasswordChange();
    },

    checkForcedPasswordChange() {
        if (this.user && this.user.must_change_password) this.showForcePasswordModal();
    },

    showForcePasswordModal() {
        UI.showModal(I18N.t('app.change_password_title'), `
            <p class="text-muted" style="margin-bottom:12px">${I18N.t('app.change_password_subtitle')}</p>
            <div class="form-group"><label>${I18N.t('app.old_password_label')}</label>${UI.passwordField('force-old-pw', I18N.t('app.old_password_placeholder'))}</div>
            <div class="form-group"><label>${I18N.t('app.new_password_label')}</label>${UI.passwordField('force-new-pw', I18N.t('auth.password_min_placeholder'))}</div>`,
            `<button class="btn btn-primary btn-block" onclick="App.saveForcedPassword()">${I18N.t('common.save')}</button>`,
            true);
    },

    async saveForcedPassword() {
        const oldPw = document.getElementById('force-old-pw').value;
        const newPw = document.getElementById('force-new-pw').value;
        if (!oldPw || newPw.length < 6) { UI.toast(I18N.t('app.old_new_required'), 'error'); return; }
        try {
            const updated = await API.auth.updateProfile(this.user.full_name, oldPw, newPw);
            this.user = updated;
            UI.closeModal();
            UI.toast(I18N.t('app.password_changed'), 'success');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async checkAuth() {
        try { this.user = await API.auth.me(); } catch { this.user = null; }
    },

    async loadProjects() {
        try { this.projects = await API.projects.list(); } catch { this.projects = []; }
    },

    navigate(page, replace) {
        const url = '/' + (page === 'files' ? '' : page);
        if (replace) history.replaceState({ page }, '', url);
        else history.pushState({ page }, '', url);
        this.route();
    },

    route() {
        const path = location.pathname.replace(/^\/+/, '') || '';
        const page = path.split('/')[0] || 'files';

        if (!this.user && !['login', 'register'].includes(page)) { this.navigate('login', true); return; }
        if (this.user && ['login', 'register'].includes(page)) { this.navigate('files', true); return; }
        if (page === 'register' && !this.config.allow_registration) { this.navigate('login', true); return; }
        if (page === 'admin' && this.user && this.user.role !== 'admin') { this.navigate('files', true); return; }

        this.renderPage(page);
    },

    async renderPage(page) {
        this.currentPage = page;
        const app = document.getElementById('app');

        if (['login', 'register'].includes(page)) {
            if (page === 'register') { app.innerHTML = AuthPage.renderRegister(); AuthPage.initRegister(); }
            else { app.innerHTML = AuthPage.renderLogin(); AuthPage.initLogin(); }
            return;
        }

        // Editor is fullscreen, no sidebar
        if (page === 'editor') {
            app.innerHTML = EditorPage.render();
            EditorPage.init();
            return;
        }
        if (page === 'preview') {
            app.innerHTML = PreviewPage.render();
            PreviewPage.init();
            return;
        }

        await this.loadProjects();
        app.innerHTML = this.renderShell(page);
        this.initPage(page);
        this.loadStorageUsage();
    },

    renderShell(page) {
        const u = this.user;
        const isAdmin = u && u.role === 'admin';
        return `
        <div class="app-layout">
            <aside class="sidebar" id="sidebar">
                <div class="sidebar-header">
                    <div class="sidebar-logo">${UI.icons.cloud} Paýlaş</div>
                </div>
                <nav class="sidebar-nav">
                    <div class="sidebar-section">${I18N.t('app.nav_main')}</div>
                    <a class="nav-item ${page === 'files' ? 'active' : ''}" onclick="App.navigate('files')">
                        ${UI.icons.folder} <span>${I18N.t('app.nav_files')}</span>
                    </a>
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'personal' ? 'active' : ''}" onclick="FilesPage.setScope('personal');App.navigate('files')">
                        <span>🔒</span> <span>${I18N.t('app.nav_personal')}</span>
                    </a>
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'common' ? 'active' : ''}" onclick="FilesPage.setScope('common');App.navigate('files')">
                        <span>🌐</span> <span>${I18N.t('app.nav_common')}</span>
                    </a>
                    ${this.projects.map(p => `
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'project' && FilesPage.currentProjectId === p.id ? 'active' : ''}" onclick="FilesPage.setScope('project',${p.id},${UI.escJson(p.name)},${UI.escJson(p.permission)});App.navigate('files')">
                        <span>${p.permission === 'view' ? '👁' : '📁'}</span> <span>${UI.esc(p.name)}</span>
                    </a>`).join('')}
                    <a class="nav-item ${page === 'shared' ? 'active' : ''}" onclick="App.navigate('shared')">
                        ${UI.icons.share} <span>${I18N.t('app.nav_shared')}</span>
                    </a>
                    <a class="nav-item ${page === 'trash' ? 'active' : ''}" onclick="App.navigate('trash')">
                        ${UI.icons.trash} <span>${I18N.t('app.nav_trash')}</span>
                    </a>
                    ${isAdmin ? `
                    <div class="sidebar-section">${I18N.t('app.nav_admin_section')}</div>
                    <a class="nav-item admin-item ${page === 'admin' ? 'active' : ''}" onclick="App.navigate('admin')">
                        ${UI.icons.settings} <span>${I18N.t('app.nav_admin_panel')}</span>
                    </a>` : ''}
                </nav>
                <div id="storage-bar" class="storage-bar"></div>
                <div class="sidebar-footer">
                    <div class="sidebar-user" style="cursor:pointer" onclick="App.showProfileModal()">
                        ${u.avatar_url ? `<img class="sidebar-avatar-img" src="/api/avatar/${u.id}?v=${Date.now()}" alt="">` : `<div class="sidebar-avatar">${(u.full_name || 'U').charAt(0).toUpperCase()}</div>`}
                        <div class="sidebar-user-info">
                            <div class="sidebar-user-name">${UI.esc(u.full_name)}</div>
                            <div class="sidebar-user-role">${u.role === 'admin' ? I18N.t('app.role_admin') : I18N.t('app.role_user')}</div>
                        </div>
                        <button class="sidebar-logout" onclick="event.stopPropagation();App.logout()" title="${I18N.t('app.logout')}" aria-label="${I18N.t('app.logout')}">${UI.icons.logout}</button>
                    </div>
                </div>
            </aside>
            <main class="main-content">
                <header class="topbar">
                    <button class="sidebar-toggle" onclick="document.getElementById('sidebar').classList.toggle('open')" aria-label="${I18N.t('app.menu_label')}">${UI.icons.menu}</button>
                    <div class="topbar-title">${this.pageTitle(page)}</div>
                    <div class="topbar-right">
                        ${UI.langSwitcher()}
                        <button class="btn btn-icon btn-ghost" id="theme-toggle" onclick="App.toggleTheme()" title="${I18N.t('app.theme')}" aria-label="${I18N.t('app.theme')}">
                            <span class="theme-icon-dark">${UI.icons.sun}</span>
                            <span class="theme-icon-light">${UI.icons.moon}</span>
                        </button>
                    </div>
                </header>
                <div class="page-content" id="page-content"></div>
            </main>
        </div>`;
    },

    pageTitle(p) {
        return { files: I18N.t('app.nav_files'), shared: I18N.t('app.nav_shared'), trash: I18N.t('app.nav_trash'), admin: I18N.t('app.nav_admin_section') }[p] || 'Paýlaş';
    },

    initPage(page) {
        const c = document.getElementById('page-content');
        if (!c) return;
        switch (page) {
            case 'files':  c.innerHTML = FilesPage.render(); FilesPage.init(); break;
            case 'shared': c.innerHTML = SharesPage.renderSharedWithMe(); SharesPage.initSharedWithMe(); break;
            case 'trash':  c.innerHTML = TrashPage.render(); TrashPage.init(); break;
            case 'admin':  c.innerHTML = AdminPage.render(); AdminPage.init(); break;
            default:       c.innerHTML = `<div class="empty-state"><p>${I18N.t('app.page_not_found')}</p></div>`;
        }
    },

    async logout() {
        try { await API.auth.logout(); } catch {}
        this.user = null;
        this.navigate('login', true);
        UI.toast(I18N.t('app.logged_out'), 'info');
    },

    async loadStorageUsage() {
        try {
            const scope = (typeof FilesPage !== 'undefined') ? FilesPage.currentScope : 'personal';
            const projectId = (typeof FilesPage !== 'undefined') ? FilesPage.currentProjectId : null;
            const d = await API.files.storageUsage(scope, projectId);
            const bar = document.getElementById('storage-bar');
            if (!bar) return;
            const pct = d.quota_bytes > 0 ? Math.min((d.used_bytes / d.quota_bytes) * 100, 100) : 0;
            const label = scope === 'project' ? (FilesPage.currentProjectName || I18N.t('app.project_label')) : scope === 'common' ? I18N.t('app.nav_common') : I18N.t('app.nav_personal');
            bar.innerHTML = `<div class="storage-info"><span>${label}: ${UI.formatBytes(d.used_bytes)} / ${UI.formatBytes(d.quota_bytes)}</span><span>${Math.round(pct)}%</span></div>
                <div class="storage-track"><div class="storage-fill ${pct > 90 ? 'danger' : pct > 70 ? 'warning' : ''}" style="width:${pct}%"></div></div>`;
        } catch {}
    },

    initTheme() {
        const saved = localStorage.getItem('paylash-theme');
        if (saved === 'light') document.documentElement.classList.add('light');
    },

    toggleTheme() {
        const isLight = document.documentElement.classList.toggle('light');
        localStorage.setItem('paylash-theme', isLight ? 'light' : 'dark');
    },

    showProfileModal() {
        const u = this.user;
        const avatarHTML = u.avatar_url
            ? `<img class="profile-avatar-img" src="/api/avatar/${u.id}?v=${Date.now()}" alt="">`
            : `<div class="profile-avatar-placeholder">${(u.full_name || 'U').charAt(0).toUpperCase()}</div>`;
        UI.showModal(I18N.t('app.profile_title'), `
            <div class="profile-avatar-section">
                <div class="profile-avatar-wrap" onclick="document.getElementById('avatar-input').click()">
                    ${avatarHTML}
                    <div class="profile-avatar-overlay">📷</div>
                </div>
                <input type="file" id="avatar-input" accept="image/*" style="display:none" onchange="App.uploadAvatar(this)">
                <p class="text-muted" style="font-size:.75rem;margin-top:4px">${I18N.t('app.avatar_hint')}</p>
            </div>
            <div class="form-group"><label>${I18N.t('auth.fullname_label')}</label><input type="text" id="prof-name" value="${UI.esc(u.full_name)}" class="form-control"></div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <div class="form-group"><label>${I18N.t('app.old_password_label')}</label>${UI.passwordField('prof-old-pw', I18N.t('app.old_password_placeholder'))}</div>
            <div class="form-group"><label>${I18N.t('app.new_password_label')}</label>${UI.passwordField('prof-new-pw', I18N.t('auth.password_min_placeholder'))}</div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="App.saveProfile()">${I18N.t('common.save')}</button>`);
    },

    async uploadAvatar(input) {
        const file = input.files[0];
        if (!file) return;
        if (!file.type.startsWith('image/')) { UI.toast(I18N.t('app.only_images_allowed'), 'error'); return; }
        if (file.size > 5 * 1024 * 1024) { UI.toast(I18N.t('app.avatar_too_large'), 'error'); return; }
        try {
            const updated = await API.auth.uploadAvatar(file);
            this.user = updated;
            UI.toast(I18N.t('app.avatar_changed'), 'success');
            UI.closeModal();
            this.renderPage(this.currentPage);
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async saveProfile() {
        const name = document.getElementById('prof-name').value.trim();
        const oldPw = document.getElementById('prof-old-pw').value;
        const newPw = document.getElementById('prof-new-pw').value;
        if (!name) { UI.toast(I18N.t('app.name_required'), 'error'); return; }
        if (newPw && !oldPw) { UI.toast(I18N.t('app.old_password_required'), 'error'); return; }
        try {
            const updated = await API.auth.updateProfile(name, oldPw, newPw);
            this.user = updated;
            UI.closeModal();
            UI.toast(I18N.t('app.profile_updated'), 'success');
            this.renderPage(this.currentPage);
        } catch (e) { UI.toast(e.message, 'error'); }
    }
};

document.addEventListener('DOMContentLoaded', () => App.start());
