/* Paylash — Main App Router */
const App = {
    user: null,
    currentPage: null,
    projects: [],
    config: { allow_registration: true },

    async start() {
        this.initTheme();
        // Public config (incl. the VAPID push key) loads BEFORE checkAuth so
        // that Push.init(), fired from checkAuth, already has the key it needs.
        try { this.config = await API.public.config(); } catch {}
        await this.checkAuth();
        window.addEventListener('popstate', () => this.route());
        this.route();
        this._handlePushHash();
        this.checkForcedPasswordChange();
        this.maybeShowOnboarding();
    },

    // A notification click on a cold-started app arrives as a hash in the
    // URL — #chat/<id> for a chat message, #shared for a share notification
    // (see pushShareNotification server-side and sw.js's generic `url`
    // field) — open the right page, then clean the hash back out. Also
    // called directly (not just at boot) by push.js's already-open-tab
    // message handler, reusing the same routing instead of duplicating it.
    _handlePushHash() {
        const chatMatch = location.hash.match(/^#chat\/(\d+)/);
        if (chatMatch && this.user) {
            history.replaceState(null, '', '/');
            this.navigate('chat', false, { c: chatMatch[1] });
            return;
        }
        if (location.hash === '#shared' && this.user) {
            history.replaceState(null, '', '/');
            this.navigate('shared');
        }
    },

    checkForcedPasswordChange() {
        if (this.user && this.user.must_change_password) this.showForcePasswordModal();
    },

    // Shown once per account, right after any mandatory password change is
    // out of the way (never stacked on top of it) — see saveForcedPassword,
    // and the login/register handlers in auth.js which call this too since
    // App.start()'s own checkForcedPasswordChange/maybeShowOnboarding pair
    // only re-runs on a full page load, not after an in-SPA login/register.
    maybeShowOnboarding() {
        if (this.user && !this.user.must_change_password && !this.user.onboarding_completed) this.showOnboarding();
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
        if (!oldPw || newPw.length < 8) { UI.toast(I18N.t('app.old_new_required'), 'error'); return; }
        try {
            const updated = await API.auth.updateProfile(this.user.full_name, oldPw, newPw);
            this.user = updated;
            UI.closeModal();
            UI.toast(I18N.t('app.password_changed'), 'success');
            // Every API call made while must_change_password was still true
            // 403'd server-side (see AuthMiddleware) — files/projects/storage
            // usage never actually loaded behind the modal. Re-render now
            // that the block is lifted so the shell shows real content
            // instead of whatever error state those failed calls left behind.
            // route() (not a plain re-render) so any folder/tab/conversation
            // already reflected in the URL survives this refresh too.
            this.route();
            this.maybeShowOnboarding();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    // ─── Onboarding (first-login welcome tour) ───
    _onboardingStep: 0,
    onboardingSteps() {
        return [
            { icon: UI.icons.cloud, title: I18N.t('onboarding.step1_title'), body: I18N.t('onboarding.step1_body') },
            { icon: UI.icons.folder, title: I18N.t('onboarding.step2_title'), body: I18N.t('onboarding.step2_body') },
            { icon: UI.icons.share, title: I18N.t('onboarding.step3_title'), body: I18N.t('onboarding.step3_body') },
            { icon: '💬', title: I18N.t('onboarding.step4_title'), body: I18N.t('onboarding.step4_body') },
            { icon: UI.icons.trash, title: I18N.t('onboarding.step5_title'), body: I18N.t('onboarding.step5_body') },
        ];
    },
    showOnboarding() {
        this._onboardingStep = 0;
        this.renderOnboardingStep();
    },
    renderOnboardingStep() {
        const steps = this.onboardingSteps();
        const step = steps[this._onboardingStep];
        const isLast = this._onboardingStep === steps.length - 1;
        const dots = steps.map((_, i) => `<span class="onboarding-dot ${i === this._onboardingStep ? 'active' : ''}"></span>`).join('');
        // hideClose (no X, no Escape) — same reasoning as showForcePasswordModal:
        // dismissal must go through Skip, or it'd silently never mark
        // onboarding_completed and the tour would just reappear next login.
        UI.showModal(step.title, `
            <div class="onboarding-body">
                <div class="onboarding-icon">${step.icon}</div>
                <p>${UI.esc(step.body)}</p>
            </div>
            <div class="onboarding-dots">${dots}</div>`,
            `<button class="btn btn-ghost" onclick="App.finishOnboarding()">${I18N.t('onboarding.skip')}</button>
             <button class="btn btn-primary" onclick="App.onboardingNext()">${isLast ? I18N.t('onboarding.finish') : I18N.t('onboarding.next')}</button>`,
            true);
    },
    onboardingNext() {
        const steps = this.onboardingSteps();
        if (this._onboardingStep < steps.length - 1) { this._onboardingStep++; this.renderOnboardingStep(); }
        else this.finishOnboarding();
    },
    async finishOnboarding() {
        UI.closeModal();
        if (this.user) this.user.onboarding_completed = true;
        try { await API.auth.completeOnboarding(); } catch (e) { console.error('completeOnboarding failed', e); }
    },

    async checkAuth() {
        try { this.user = await API.auth.me(); } catch { this.user = null; }
        if (this.user) {
            this.startNotifPolling();
            this.startChatUnreadPolling();
            this._loadMutedConvIds();
            this._bindChatSocketListeners();
            ChatSocket.connect();
            if (typeof Push !== 'undefined') Push.init();
        } else {
            this.stopNotifPolling();
            this.stopChatUnreadPolling();
            this._mutedConvIds = new Set();
            ChatSocket.disconnect();
        }
    },

    // Cache of the current user's own muted conversation ids — fetched once
    // at login and kept in sync via the "conversation.prefs" WS event (see
    // _bindChatSocketListeners) plus ChatPage.setConvPrefs updating it
    // instantly on the tab that actually toggled it. Consulted below so a
    // muted chat never pops a toast/native notification/sound, regardless of
    // whether the Chat page itself has been opened yet this session.
    _mutedConvIds: new Set(),

    async _loadMutedConvIds() {
        try { this._mutedConvIds = new Set(await API.chat.listMuted() || []); }
        catch { /* transient — worst case a muted chat isn't silenced until next load */ }
    },

    /* ── Shared-file notifications ──
       No WebSocket/SSE push in this app, so "new share" notifications are
       just periodic polling of a cheap unread-count endpoint — good enough
       for a small internal tool, and much simpler/more robust than holding
       a live connection open per user. */
    _lastNotifCount: 0,
    _notifPollHandle: null,

    startNotifPolling() {
        if (this._notifPollHandle) return;
        this.checkNotifications(true);
        this._notifPollHandle = setInterval(() => this.checkNotifications(false), 30000);
    },

    stopNotifPolling() {
        if (this._notifPollHandle) { clearInterval(this._notifPollHandle); this._notifPollHandle = null; }
        this._lastNotifCount = 0;
    },

    async checkNotifications(isInitial) {
        try {
            const { count } = await API.notifications.unreadCount();
            if (!isInitial && count > this._lastNotifCount) {
                const delta = count - this._lastNotifCount;
                UI.toast(I18N.tn('notifications.new_shares_toast', delta), 'info');
            }
            this._lastNotifCount = count;
            this.renderNotifBadge(count);
        } catch { /* transient network hiccup — next poll tries again */ }
    },

    renderNotifBadge(count) {
        const el = document.getElementById('shared-notif-badge');
        if (!el) return;
        el.textContent = count > 99 ? '99+' : String(count);
        el.classList.toggle('hidden', count <= 0);
    },

    /* ── Chat ──
       Real-time delivery comes from ChatSocket (see chatSocket.js), but this
       slow fallback poll is kept alongside it — same resilience reasoning
       as the share-notification poll above — for the rare stuck-reconnect
       case, and because it's also how the badge gets its very first value
       before any WS event has arrived. */
    _lastChatUnread: 0,
    _chatUnreadPollHandle: null,
    _chatSocketListenersBound: false,

    startChatUnreadPolling() {
        if (this._chatUnreadPollHandle) return;
        this.checkChatUnread();
        this._chatUnreadPollHandle = setInterval(() => this.checkChatUnread(), 60000);
    },

    stopChatUnreadPolling() {
        if (this._chatUnreadPollHandle) { clearInterval(this._chatUnreadPollHandle); this._chatUnreadPollHandle = null; }
        this._lastChatUnread = 0;
    },

    async checkChatUnread() {
        try {
            const { count } = await API.chat.unreadCount();
            this._lastChatUnread = count;
            this.renderChatBadge(count);
        } catch { /* transient network hiccup — next poll/WS event tries again */ }
    },

    renderChatBadge(count) {
        const el = document.getElementById('chat-notif-badge');
        if (!el) return;
        el.textContent = count > 99 ? '99+' : String(count);
        el.classList.toggle('hidden', count <= 0);
    },

    // Bound once for the whole session (ChatSocket itself persists across
    // page navigation, unlike a page object) — refreshes the badge on every
    // relevant event and alerts the user for messages that aren't their own
    // and aren't already on-screen. Two-tier alert, matching how Telegram
    // Desktop actually behaves: a rich in-app toast while the tab itself is
    // focused (a native OS toast reads as out of place right then), a native
    // OS toast otherwise — both gated by the user's own notification-privacy
    // level (full / sender-only / hidden, set in the profile modal).
    _bindChatSocketListeners() {
        if (this._chatSocketListenersBound) return;
        this._chatSocketListenersBound = true;
        ChatSocket.on('message.new', (data) => {
            if (App.user && data.message && data.message.sender_id === App.user.id) return;
            App.checkChatUnread();
            const viewingThisConv = App.currentPage === 'chat' && typeof ChatPage !== 'undefined' && ChatPage._activeId === data.conversation_id;
            const focused = document.hasFocus() && !document.hidden;
            if (viewingThisConv && focused) return; // already looking right at it — no alert needed
            // Muted: still counts toward unread (checkChatUnread above already
            // ran) but never pops a toast/sound/native notification.
            if (App._mutedConvIds.has(data.conversation_id)) return;

            const level = (App.user && App.user.chat_notify_level) || 'full';
            const senderName = data.message ? (data.message.sender_name || I18N.t('chat.title')) : I18N.t('chat.title');
            let title, body;
            if (level === 'hidden') {
                title = I18N.t('chat.title');
                body = I18N.t('chat.new_message_generic');
            } else if (level === 'sender_only') {
                title = senderName;
                body = I18N.t('chat.new_message_generic');
            } else {
                title = senderName;
                body = data.message && data.message.kind === 'sticker' ? I18N.t('chat.sticker_notification')
                    : data.message && data.message.body ? data.message.body
                    : I18N.t('chat.attachment_notification');
            }

            if (!App.user || App.user.chat_notify_sound) ChatSocket.playChime();
            if (focused) {
                ChatSocket.showInAppToast({
                    avatarUserId: data.message && data.message.sender_id, avatarName: senderName,
                    avatarUrl: data.message && data.message.sender_avatar,
                    title, body, conversationId: data.conversation_id,
                });
            } else {
                ChatSocket.notify({ title, body, conversationId: data.conversation_id });
            }
        });
        ChatSocket.on('message.deleted', () => App.checkChatUnread());
        ChatSocket.on('conversation.updated', () => App.checkChatUnread());
        // Mirrors ChatPage's own "conversation.prefs" handler (self-sync
        // across this same user's other tabs/devices) — kept independently
        // here too since this cache must stay correct even before the Chat
        // page has ever been opened this session.
        ChatSocket.on('conversation.prefs', (data) => {
            if (data.muted === true) App._mutedConvIds.add(data.conversation_id);
            else if (data.muted === false) App._mutedConvIds.delete(data.conversation_id);
        });
    },

    // Regular employees see only projects they're a member of (ListMyProjects).
    // Admins additionally see every OTHER project too — the backend already
    // grants admins full edit access to any project regardless of membership
    // (see canEditScopeWith server-side), so surfacing them here means an
    // admin browses/manages project files through the exact same Files page
    // and its full toolset (multi-select, move, share, versions, sort,
    // filter, search) everyone else uses, rather than a separate, more
    // limited "admin file browser" — see the project's audit notes on why
    // that duplicate implementation was retired in favor of this.
    async loadProjects() {
        try { this.projects = await API.projects.list(); } catch { this.projects = []; }
        if (this.user && this.user.role === 'admin') {
            try {
                const all = await API.admin.projects.list();
                const known = new Set(this.projects.map(p => p.id));
                for (const p of all) {
                    if (!known.has(p.id)) this.projects.push({ id: p.id, name: p.name, permission: 'edit' });
                }
                this.projects.sort((a, b) => a.name.localeCompare(b.name));
            } catch { /* best-effort — worst case an admin only sees their own memberships this load */ }
        }
    },

    // params (a plain object of query-string key/values) becomes the new
    // URL's query string — how a page's own sub-state (files: folder/scope/
    // project; chat: open conversation; admin: active tab) survives a
    // refresh or a browser back/forward instead of always resetting to that
    // page's default view. See updatePageURL for changing just the query
    // string of the CURRENT page without a full route()/re-render.
    navigate(page, replace, params) {
        let url = '/' + (page === 'files' ? '' : page);
        const qs = params ? new URLSearchParams(params).toString() : '';
        if (qs) url += '?' + qs;
        if (replace) history.replaceState({ page }, '', url);
        else history.pushState({ page }, '', url);
        this.route();
    },

    // Updates the query string for whatever page is ALREADY showing, without
    // tearing down and re-rendering the page shell — used for in-page state
    // changes (opening a folder, switching admin tab, opening a chat
    // conversation) that already update themselves via a lighter, targeted
    // DOM patch. push=true adds a new history entry (so browser Back
    // retraces the step, appropriate for hierarchical drill-down like file
    // folders); push=false (the default) replaces the current entry
    // (appropriate for tab-like switches — chat conversation, admin tab —
    // where retracing every switch via Back would be more annoying than
    // useful, matching how Telegram Web/Slack's own URL handling behaves).
    updatePageURL(params, push) {
        let url = '/' + (this.currentPage === 'files' ? '' : this.currentPage);
        const qs = params ? new URLSearchParams(params).toString() : '';
        if (qs) url += '?' + qs;
        const method = push ? 'pushState' : 'replaceState';
        history[method]({ page: this.currentPage }, '', url);
    },

    route() {
        const path = location.pathname.replace(/^\/+/, '') || '';
        const page = path.split('/')[0] || 'files';
        const params = new URLSearchParams(location.search);

        if (!this.user && !['login', 'register'].includes(page)) { this.navigate('login', true); return; }
        if (this.user && ['login', 'register'].includes(page)) { this.navigate('files', true); return; }
        if (page === 'register' && !this.config.allow_registration) { this.navigate('login', true); return; }
        if (page === 'admin' && this.user && this.user.role !== 'admin' && this.user.role !== 'manager') { this.navigate('files', true); return; }

        this.renderPage(page, params);
    },

    async renderPage(page, params) {
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
        this.initPage(page, params);
        this.loadStorageUsage();
        this.renderNotifBadge(this._lastNotifCount);
        this.renderChatBadge(this._lastChatUnread);
        AttendanceWidget.init();
    },

    renderShell(page) {
        const u = this.user;
        const isAdmin = u && u.role === 'admin';
        const isManager = u && u.role === 'manager';
        return `
        <div class="app-layout">
            <aside class="sidebar" id="sidebar">
                <div class="sidebar-header">
                    <div class="sidebar-logo">${UI.icons.cloud} Paýlaş</div>
                </div>
                <div class="attn-widget" id="attendance-widget"></div>
                <nav class="sidebar-nav">
                    <div class="sidebar-section">${I18N.t('app.nav_main')}</div>
                    <a class="nav-item ${page === 'files' ? 'active' : ''}" onclick="App.navigate('files')">
                        ${UI.icons.folder} <span>${I18N.t('app.nav_files')}</span>
                    </a>
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'personal' ? 'active' : ''}" onclick="FilesPage.setScope('personal')">
                        <span>🔒</span> <span>${I18N.t('app.nav_personal')}</span>
                    </a>
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'common' ? 'active' : ''}" onclick="FilesPage.setScope('common')">
                        <span>🌐</span> <span>${I18N.t('app.nav_common')}</span>
                    </a>
                    ${this.projects.map(p => `
                    <a class="nav-item nav-sub ${page === 'files' && FilesPage.currentScope === 'project' && FilesPage.currentProjectId === p.id ? 'active' : ''}" onclick="FilesPage.setScope('project',${p.id},${UI.escJson(p.name)},${UI.escJson(p.permission)})">
                        <span>${p.permission === 'view' ? '👁' : '📁'}</span> <span>${UI.esc(p.name)}</span>
                    </a>`).join('')}
                    <a class="nav-item ${page === 'shared' ? 'active' : ''}" onclick="App.navigate('shared')">
                        ${UI.icons.share} <span>${I18N.t('app.nav_shared')}</span>
                        <span class="nav-badge hidden" id="shared-notif-badge"></span>
                    </a>
                    <a class="nav-item ${page === 'chat' ? 'active' : ''}" onclick="App.navigate('chat')">
                        💬 <span>${I18N.t('app.nav_chat')}</span>
                        <span class="nav-badge hidden" id="chat-notif-badge"></span>
                    </a>
                    <a class="nav-item ${page === 'trash' ? 'active' : ''}" onclick="App.navigate('trash')">
                        ${UI.icons.trash} <span>${I18N.t('app.nav_trash')}</span>
                    </a>
                    <a class="nav-item ${page === 'attendance' ? 'active' : ''}" onclick="App.navigate('attendance')">
                        🕐 <span>${I18N.t('app.nav_attendance')}</span>
                    </a>
                    ${(isAdmin || isManager) ? `
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
                            <div class="sidebar-user-role">${{ admin: I18N.t('app.role_admin'), manager: I18N.t('app.role_manager') }[u.role] || I18N.t('app.role_user')}</div>
                        </div>
                        <button class="sidebar-logout" onclick="event.stopPropagation();App.logout()" title="${I18N.t('app.logout')}" aria-label="${I18N.t('app.logout')}">${UI.icons.logout}</button>
                    </div>
                </div>
            </aside>
            <main class="main-content">
                ${u.impersonator ? `<div class="impersonation-banner">
                    <span>🎭 ${I18N.t('admin.impersonation_banner', { name: UI.esc(u.full_name || u.username), admin: UI.esc(u.impersonator.full_name || u.impersonator.username) })}</span>
                    <button class="btn btn-sm" onclick="App.exitImpersonation()">${I18N.t('admin.exit_impersonation')}</button>
                </div>` : ''}
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
        return { files: I18N.t('app.nav_files'), shared: I18N.t('app.nav_shared'), trash: I18N.t('app.nav_trash'), admin: I18N.t('app.nav_admin_section'), attendance: I18N.t('app.nav_attendance') }[p] || 'Paýlaş';
    },

    initPage(page, params) {
        const c = document.getElementById('page-content');
        if (!c) return;
        switch (page) {
            case 'files':
                if (typeof FilesPage !== 'undefined') FilesPage.applyURLParams(params);
                c.innerHTML = FilesPage.render(); FilesPage.init(); break;
            case 'shared': c.innerHTML = SharesPage.renderSharedWithMe(); SharesPage.initSharedWithMe(); break;
            case 'chat':
                if (typeof ChatPage !== 'undefined') ChatPage.applyURLParams(params);
                c.innerHTML = ChatPage.render(); ChatPage.init(); break;
            case 'trash':  c.innerHTML = TrashPage.render(); TrashPage.init(); break;
            case 'attendance': c.innerHTML = AttendancePage.render(); AttendancePage.init(); break;
            case 'admin':
                if (typeof AdminPage !== 'undefined') AdminPage.applyURLParams(params);
                c.innerHTML = AdminPage.render(); AdminPage.init(); break;
            default:       c.innerHTML = `<div class="empty-state"><p>${I18N.t('app.page_not_found')}</p></div>`;
        }
    },

    async logout() {
        if (typeof Push !== 'undefined') { try { await Push.unsubscribe(); } catch {} }
        try { await API.auth.logout(); } catch {}
        this.user = null;
        this.stopNotifPolling();
        this.stopChatUnreadPolling();
        ChatSocket.disconnect();
        this.navigate('login', true);
        UI.toast(I18N.t('app.logged_out'), 'info');
    },

    // Reload rather than patch this.user in place, same reasoning as
    // AdminPage.impersonate — a session switch this large isn't worth
    // trying to unwind piecemeal in already-loaded page state.
    async exitImpersonation() {
        try {
            await API.auth.exitImpersonation();
            location.reload();
        } catch (e) { UI.toast(e.message, 'error'); }
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
        const level = u.chat_notify_level || 'full';
        const sound = u.chat_notify_sound !== false;
        const notifyOption = (value, label) => `
            <label class="radio-option">
                <input type="radio" name="notify-level" value="${value}" ${level === value ? 'checked' : ''}>
                <span>${label}</span>
            </label>`;
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
            <div class="form-group"><label>${I18N.t('app.new_password_label')}</label>${UI.passwordField('prof-new-pw', I18N.t('auth.password_min_placeholder'))}</div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <div class="form-group">
                <label>${I18N.t('app.chat_notify_level_label')}</label>
                <div class="radio-group">
                    ${notifyOption('full', I18N.t('app.chat_notify_full'))}
                    ${notifyOption('sender_only', I18N.t('app.chat_notify_sender_only'))}
                    ${notifyOption('hidden', I18N.t('app.chat_notify_hidden'))}
                </div>
            </div>
            <div class="form-group">
                <label class="checkbox-option"><input type="checkbox" id="prof-notify-sound" ${sound ? 'checked' : ''}> <span>${I18N.t('app.chat_notify_sound_label')}</span></label>
            </div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <div class="form-group">
                <label>${I18N.t('app.blocked_users_label')}</label>
                <div id="prof-blocked-list"><div class="empty-state"><div class="spinner"></div></div></div>
            </div>
            <hr style="border:none;border-top:1px solid var(--border);margin:12px 0">
            <button type="button" class="btn btn-ghost btn-sm" style="width:100%" onclick="App.logoutOtherDevices()">${I18N.t('app.logout_others_button')}</button>
            <p class="text-muted" style="font-size:.72rem;margin-top:4px">${I18N.t('app.logout_others_hint')}</p>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="App.saveProfile()">${I18N.t('common.save')}</button>`);
        this.loadBlockedUsersList();
    },

    async loadBlockedUsersList() {
        const el = document.getElementById('prof-blocked-list');
        if (!el) return;
        let list;
        try { list = await API.chat.listBlocked() || []; } catch { list = []; }
        // The modal may have been closed (or reopened as a different modal)
        // by the time this resolves — re-check the element is still ours.
        if (!document.getElementById('prof-blocked-list')) return;
        if (!list.length) {
            el.innerHTML = `<p class="text-muted" style="font-size:.8rem">${I18N.t('app.no_blocked_users')}</p>`;
            return;
        }
        el.innerHTML = list.map(u => `<div class="chat-member-row">
            ${UI.avatarHTML(u.id, u.full_name, 'chat-avatar-sm', u.avatar_url)}
            <span class="chat-member-name">${UI.esc(u.full_name || u.username)}</span>
            <button class="btn btn-ghost btn-sm" onclick="App.unblockUserFromProfile(${u.id})">${I18N.t('chat.unblock_user')}</button>
        </div>`).join('');
    },

    async unblockUserFromProfile(userId) {
        try {
            await API.chat.unblockUser(userId);
            if (typeof ChatPage !== 'undefined') ChatPage._blockedIds.delete(userId);
            this.loadBlockedUsersList();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    logoutOtherDevices() {
        UI.confirmAction(I18N.t('app.logout_others_confirm_title'), I18N.t('app.logout_others_confirm_body'), I18N.t('app.logout_others_button'), async () => {
            try {
                await API.auth.logoutOthers();
                UI.toast(I18N.t('app.logout_others_done'), 'success');
                this.showProfileModal();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
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
            this.route();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async saveProfile() {
        const name = document.getElementById('prof-name').value.trim();
        const oldPw = document.getElementById('prof-old-pw').value;
        const newPw = document.getElementById('prof-new-pw').value;
        if (!name) { UI.toast(I18N.t('app.name_required'), 'error'); return; }
        if (newPw && !oldPw) { UI.toast(I18N.t('app.old_password_required'), 'error'); return; }
        const levelInput = document.querySelector('input[name="notify-level"]:checked');
        const level = levelInput ? levelInput.value : 'full';
        const sound = document.getElementById('prof-notify-sound')?.checked !== false;
        try {
            await API.auth.updateProfile(name, oldPw, newPw);
            const updated = await API.chat.updateNotifyPrefs(level, sound);
            this.user = updated;
            UI.closeModal();
            UI.toast(I18N.t('app.profile_updated'), 'success');
            this.route();
        } catch (e) { UI.toast(e.message, 'error'); }
    }
};

document.addEventListener('DOMContentLoaded', () => App.start());
