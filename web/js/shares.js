/* Paylash — Shares Page */
const SharesPage = {
    _activeShareTab: 'with-me',
    _withMeFiles: [],
    _byMeFiles: [],
    // Which person's files are currently drilled into (their user id), or
    // null for the top-level "grid of people" view — kept per page instance
    // rather than per-tab since switching tabs always resets to the grid.
    _expandedUserId: null,

    renderSharedWithMe() {
        return `
        <div class="shares-tabs">
            <button class="shares-tab active" id="tab-with-me" onclick="SharesPage.switchShareTab('with-me')">📥 ${I18N.t('shares.tab_with_me')}</button>
            <button class="shares-tab" id="tab-by-me" onclick="SharesPage.switchShareTab('by-me')">📤 ${I18N.t('shares.tab_by_me')}</button>
        </div>
        <div id="shares-tab-content">${UI.skeletonCards(3)}</div>`;
    },

    async initSharedWithMe() {
        this._activeShareTab = 'with-me';
        this._expandedUserId = null;
        this.loadSharedWithMe();
        // Visiting this tab is the "I've seen it" signal — clear the badge
        // immediately client-side rather than waiting for the next poll.
        if (typeof App !== 'undefined') {
            API.notifications.markSeen().then(() => {
                App._lastNotifCount = 0;
                App.renderNotifBadge(0);
            }).catch(() => {});
        }
    },

    switchShareTab(tab) {
        this._activeShareTab = tab;
        this._expandedUserId = null;
        document.querySelectorAll('.shares-tab').forEach(b => b.classList.toggle('active', b.id === `tab-${tab}`));
        const c = document.getElementById('shares-tab-content');
        if (c) c.innerHTML = UI.skeletonCards(3);
        if (tab === 'with-me') this.loadSharedWithMe();
        else this.loadSharedByMe();
    },

    async loadSharedByMe() {
        const c = document.getElementById('shares-tab-content');
        if (!c) return;
        try {
            this._byMeFiles = (await API.sharing.sharedByMe()) || [];
            this.renderShareGroups();
        } catch (err) {
            c.innerHTML = `<p class="text-muted">${UI.esc(err.message)}</p>`;
        }
    },

    async loadSharedWithMe() {
        const c = document.getElementById('shares-tab-content');
        if (!c) return;
        try {
            this._withMeFiles = (await API.sharing.sharedWithMe()) || [];
            this.renderShareGroups();
        } catch (err) {
            c.innerHTML = `<p class="text-muted">${UI.esc(err.message)}</p>`;
        }
    },

    // Both tabs share one rendering path: the API already returns one row
    // per (file, person) pair, so grouping by that other person — who
    // shared with me, or who I shared with — turns into a simple key-by.
    // A flat file list made it impossible to see at a glance who's on the
    // other end of a share, especially once more than a couple of colleagues
    // were involved; grouping by avatar first, with a drill-down into that
    // person's files, scales much better.
    renderShareGroups() {
        const c = document.getElementById('shares-tab-content');
        if (!c) return;
        const isWithMe = this._activeShareTab === 'with-me';
        const files = isWithMe ? this._withMeFiles : this._byMeFiles;
        const idKey = isWithMe ? 'shared_by_id' : 'shared_with_id';
        const nameKey = isWithMe ? 'shared_by_name' : 'shared_with_name';

        if (!files.length) {
            const icon = isWithMe ? '📥' : '📤';
            const text = isWithMe ? I18N.t('shares.empty_with_me') : I18N.t('shares.empty_by_me');
            c.innerHTML = `<div class="shares-empty"><div class="shares-empty-icon">${icon}</div><p>${text}</p></div>`;
            return;
        }

        if (this._expandedUserId != null) {
            const userFiles = files.filter(f => f[idKey] === this._expandedUserId);
            if (!userFiles.length) { this._expandedUserId = null; return this.renderShareGroups(); }
            const name = userFiles[0][nameKey] || '';
            c.innerHTML = `
                <div class="share-group-header">
                    <button class="btn btn-icon btn-ghost btn-sm" onclick="SharesPage.collapseGroup()" title="${I18N.t('shares.back_to_users')}" aria-label="${I18N.t('shares.back_to_users')}">${UI.icons.back}</button>
                    ${UI.avatarHTML(this._expandedUserId, name, 'share-user-avatar-lg')}
                    <span class="share-group-title">${UI.esc(name)}</span>
                </div>
                <div class="file-grid">${userFiles.map(f => this.shareFileCard(f)).join('')}</div>`;
            return;
        }

        const groups = new Map();
        for (const f of files) {
            const id = f[idKey];
            if (!groups.has(id)) groups.set(id, { id, name: f[nameKey] || '', count: 0 });
            groups.get(id).count++;
        }
        const sorted = [...groups.values()].sort((a, b) => a.name.localeCompare(b.name));
        c.innerHTML = `<div class="share-user-grid">` + sorted.map(g => `
            <div class="share-user-card" onclick="SharesPage.expandGroup(${g.id})">
                ${UI.avatarHTML(g.id, g.name, 'share-user-avatar-lg')}
                <div class="share-user-card-name" title="${UI.esc(g.name)}">${UI.esc(g.name)}</div>
                <div class="share-user-card-count">${I18N.tn('shares.file_count', g.count)}</div>
            </div>`).join('') + `</div>`;
    },

    expandGroup(userId) {
        this._expandedUserId = userId;
        this.renderShareGroups();
    },

    collapseGroup() {
        this._expandedUserId = null;
        this.renderShareGroups();
    },

    shareFileCard(f) {
        const ext = f.name.split('.').pop().toLowerCase();
        const iconHtml = UI.isThumbnailable(ext)
            ? `<img class="file-card-thumb" src="/api/files/${f.id}/thumbnail?v=${f.version || 0}" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
            : UI.isImage(ext)
            ? `<img class="file-card-thumb" src="/api/files/${f.id}/download" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
            : `<div class="file-card-icon document">${UI.fileIcon(f.name, false)}</div>`;
        const dbl = `UI.openFile(${f.id},${UI.escJson(f.name)},${f.size_bytes || 0})`;
        const badge = f.permission === 'edit' ? `✏️ ${I18N.t('shares.can_edit')}` : `👁 ${I18N.t('shares.view_only')}`;
        return `<div class="file-card" ondblclick="${dbl}">
            ${iconHtml}
            <div class="file-card-name" title="${UI.esc(f.name)}">${UI.esc(f.name)}</div>
            <div class="file-card-meta">${UI.formatBytes(f.size_bytes || 0)}</div>
            <div class="file-card-badge">${badge}</div>
        </div>`;
    },

    /* ── Share Modal (multi-user, single or multiple files) ── */

    _currentFiles: [],
    _pendingRecipients: [],
    _existingShareUserIds: [],
    _selectedVisibility: 'private',

    // fileOrFiles is a single file object (from a context menu / preview /
    // editor toolbar) or an array of file objects (from the files-page bulk
    // action bar) — normalized to an array internally either way.
    showShareModal(fileOrFiles) {
        const files = Array.isArray(fileOrFiles) ? fileOrFiles : [fileOrFiles];
        if (!files.length) return;
        const single = files.length === 1 ? files[0] : null;
        const isAdmin = App.user && App.user.role === 'admin';
        const vis = single ? (single.visibility || 'private') : 'private';

        let visibilityHTML = '';
        if (single && isAdmin) {
            visibilityHTML = `
            <hr style="border:none;border-top:1px solid var(--border);margin:14px 0">
            <div class="form-group">
                <label>${I18N.t('shares.visibility_label')}</label>
                <div class="visibility-buttons" id="vis-buttons">
                    <button class="btn btn-sm vis-btn ${vis === 'private' ? 'active' : ''}" data-vis="private" onclick="SharesPage.selectVisibility('private')">🔒 ${I18N.t('shares.vis_private')}</button>
                    <button class="btn btn-sm vis-btn ${vis === 'common' ? 'active' : ''}" data-vis="common" onclick="SharesPage.selectVisibility('common')">🌐 ${I18N.t('shares.vis_common')}</button>
                </div>
                <div id="vis-status" class="vis-status" style="font-size:.78rem;color:var(--text-3);margin-top:6px">
                    ${vis === 'common' ? I18N.t('shares.vis_common_hint') : I18N.t('shares.vis_private_hint')}
                </div>
            </div>`;
        }

        const title = single ? I18N.t('shares.modal_title', { name: single.name }) : I18N.t('shares.modal_title_multi', { count: files.length });
        const existingSection = single
            ? `<div id="share-existing"><p class="text-muted">${I18N.t('common.loading')}</p></div>`
            : `<p class="text-muted" style="font-size:.78rem">${I18N.t('shares.bulk_hint', { count: files.length })}</p>`;

        UI.showModal(title, `
            <div class="form-group share-user-picker">
                <label>${I18N.t('shares.search_label')}</label>
                <input type="text" id="share-user-search" class="form-control" placeholder="${I18N.t('shares.search_placeholder')}"
                    oninput="SharesPage.searchUsers(this.value)" onfocus="SharesPage.onSearchFocus(this.value)" autocomplete="off">
                <div id="share-user-results" class="share-user-results"></div>
            </div>
            <div class="form-group">
                <label>${I18N.t('shares.permission_label')}</label>
                <select id="share-permission" class="form-control">
                    <option value="view">👁 ${I18N.t('shares.perm_view_option')}</option>
                    <option value="edit">✏️ ${I18N.t('shares.perm_edit_option')}</option>
                </select>
            </div>
            <div id="share-pending-list"></div>
            ${visibilityHTML}
            ${existingSection}`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="SharesPage.saveShare()">${I18N.t('common.save')}</button>`);

        this._currentFiles = files;
        this._pendingRecipients = [];
        this._existingShareUserIds = [];
        this._selectedVisibility = vis;
        this._bindDropdownClose();
        if (single) this.loadExistingShares(single.id);
    },

    // The results dropdown (populated either by typing or by focusing the
    // empty field — see onSearchFocus) stays open until an explicit pick or
    // a click elsewhere; this closes it on any outside click. Bound once for
    // the page's lifetime — showModal wipes and rebuilds the DOM on every
    // open, so a per-open listener would leak one more each time.
    _dropdownCloseBound: false,
    _bindDropdownClose() {
        if (this._dropdownCloseBound) return;
        this._dropdownCloseBound = true;
        document.addEventListener('click', ev => {
            const input = document.getElementById('share-user-search');
            const results = document.getElementById('share-user-results');
            if (!input || !results) return;
            if (ev.target === input || results.contains(ev.target)) return;
            results.innerHTML = '';
        });
    },

    // Focusing the empty search field browses the full people list (a
    // dropdown-style picker) rather than requiring the user to already know
    // who to type — typing still narrows it exactly as before.
    onSearchFocus(q) {
        if (!q) this.searchUsers('', true);
    },

    async searchUsers(q, browseAll) {
        const r = document.getElementById('share-user-results');
        if (!r) return;
        if (!browseAll && (!q || q.length < 2)) { r.innerHTML = ''; return; }
        try {
            const users = await API.sharing.searchUsers(q || '');
            if (!users?.length) { r.innerHTML = `<div class="share-user-no-result">${I18N.t('shares.no_results')}</div>`; return; }
            // Filter out already-pending and already-shared users
            const skipIds = new Set([
                ...this._pendingRecipients.map(p => p.id),
                ...this._existingShareUserIds,
                App.user?.id
            ].filter(Boolean));
            const filtered = users.filter(u => !skipIds.has(u.id));
            if (!filtered.length) { r.innerHTML = `<div class="share-user-no-result">${I18N.t('shares.no_results')}</div>`; return; }
            r.innerHTML = filtered.map(u => `
                <div class="share-user-item" onclick="SharesPage.addRecipient(${u.id},${UI.escJson(u.full_name)},${UI.escJson(u.username)})">
                    ${UI.avatarHTML(u.id, u.full_name)}
                    <div><div class="share-user-name">${UI.esc(u.full_name)}</div><div class="share-user-username">@${UI.esc(u.username)}</div></div>
                </div>`).join('');
        } catch { r.innerHTML = ''; }
    },

    addRecipient(id, name, username) {
        if (this._pendingRecipients.some(p => p.id === id)) return;
        const perm = document.getElementById('share-permission')?.value || 'view';
        this._pendingRecipients.push({ id, name, username, permission: perm });
        document.getElementById('share-user-search').value = '';
        document.getElementById('share-user-results').innerHTML = '';
        this.renderPendingList();
    },

    removeRecipient(id) {
        this._pendingRecipients = this._pendingRecipients.filter(p => p.id !== id);
        this.renderPendingList();
    },

    updateRecipientPermission(id, perm) {
        const r = this._pendingRecipients.find(p => p.id === id);
        if (r) r.permission = perm;
    },

    renderPendingList() {
        const el = document.getElementById('share-pending-list');
        if (!el) return;
        if (!this._pendingRecipients.length) { el.innerHTML = ''; return; }
        el.innerHTML = `<h4 style="font-size:.82rem;font-weight:600;color:var(--text-2);margin:8px 0">${I18N.t('shares.new_shares_heading')}</h4>` +
            this._pendingRecipients.map(p => `
            <div class="share-pending-item">
                ${UI.avatarHTML(p.id, p.name)}
                <div style="flex:1;min-width:0"><div style="font-size:.82rem;font-weight:500">${UI.esc(p.name)}</div></div>
                <select class="form-control" style="width:auto;padding:4px 28px 4px 8px;font-size:.72rem" onchange="SharesPage.updateRecipientPermission(${p.id},this.value)">
                    <option value="view" ${p.permission === 'view' ? 'selected' : ''}>👁 ${I18N.t('shares.perm_view_option')}</option>
                    <option value="edit" ${p.permission === 'edit' ? 'selected' : ''}>✏️ ${I18N.t('shares.perm_edit_option')}</option>
                </select>
                <button class="btn btn-icon btn-sm btn-danger" onclick="SharesPage.removeRecipient(${p.id})" title="${I18N.t('shares.remove_title')}">✕</button>
            </div>`).join('');
    },

    selectVisibility(vis) {
        this._selectedVisibility = vis;
        document.querySelectorAll('.vis-btn').forEach(b => b.classList.toggle('active', b.dataset.vis === vis));
        const st = document.getElementById('vis-status');
        if (st) st.textContent = vis === 'common' ? I18N.t('shares.vis_common_hint') : I18N.t('shares.vis_private_hint');
    },

    async saveShare() {
        if (!this._currentFiles.length) return;
        try {
            // Visibility only applies to a single-file share (see showShareModal).
            if (this._currentFiles.length === 1) {
                const file = this._currentFiles[0];
                const newVis = this._selectedVisibility || 'private';
                const oldVis = file.visibility || 'private';
                if (newVis !== oldVis) {
                    await API.sharing.setVisibility(file.id, newVis);
                    file.visibility = newVis;
                }
            }
            // Share every selected file with every pending recipient.
            const errors = [];
            let shareCount = 0;
            for (const file of this._currentFiles) {
                for (const p of this._pendingRecipients) {
                    try {
                        await API.sharing.share(file.id, p.id, p.permission);
                        shareCount++;
                    } catch (e) { errors.push(`${file.name} → ${p.name}: ${e.message}`); }
                }
            }
            UI.closeModal();
            if (errors.length) {
                UI.toast(I18N.t('shares.some_errors', { errors: errors.join('; ') }), 'error');
            } else if (shareCount) {
                UI.toast(I18N.t('shares.shared_with_count', { count: this._pendingRecipients.length }), 'success');
            } else {
                UI.toast(I18N.t('shares.changes_saved'), 'success');
            }
            this._pendingRecipients = [];
            if (typeof FilesPage !== 'undefined') { FilesPage.clearSelection?.(); FilesPage.loadFiles(); }
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async loadExistingShares(fileId) {
        const el = document.getElementById('share-existing');
        if (!el) return;
        try {
            const shares = await API.sharing.getFileShares(fileId);
            this._existingShareUserIds = (shares || []).filter(s => s.shared_with).map(s => s.shared_with);
            if (!shares?.length) { el.innerHTML = `<p class="text-muted" style="font-size:.82rem">${I18N.t('shares.existing_none')}</p>`; return; }
            el.innerHTML = `<h4 style="font-size:.82rem;font-weight:600;color:var(--text-2);margin-bottom:8px">${I18N.t('shares.existing_heading')}</h4>` +
                shares.map(s => `
                <div class="share-existing-item">
                    ${UI.avatarHTML(s.shared_with, s.full_name || s.username)}
                    <div class="share-existing-info">
                        <div style="font-size:.82rem;font-weight:500">${UI.esc(s.full_name || s.username)}</div>
                    </div>
                    <select class="form-control" style="width:auto;padding:4px 28px 4px 8px;font-size:.72rem" onchange="SharesPage.updatePermission(${fileId},${s.shared_with},this.value)">
                        <option value="view" ${s.permission === 'view' ? 'selected' : ''}>👁 ${I18N.t('shares.perm_view_option')}</option>
                        <option value="edit" ${s.permission === 'edit' ? 'selected' : ''}>✏️ ${I18N.t('shares.perm_edit_option')}</option>
                    </select>
                    <button class="btn btn-icon btn-sm btn-danger" onclick="SharesPage.removeShare(${fileId},${s.shared_with})" title="${I18N.t('shares.remove_title')}">✕</button>
                </div>`).join('');
        } catch { el.innerHTML = ''; }
    },

    async updatePermission(fileId, userId, permission) {
        try {
            await API.sharing.updateSharePermission(fileId, userId, permission);
            UI.toast(I18N.t('shares.permission_updated'), 'success');
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async removeShare(fileId, userId) {
        try { await API.sharing.deleteShare(fileId, userId); UI.toast(I18N.t('shares.share_removed'), 'success'); this.loadExistingShares(fileId); }
        catch (e) { UI.toast(e.message, 'error'); }
    }
};
