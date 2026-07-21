/* Paylash — Shares Page */
const SharesPage = {
    _activeShareTab: 'with-me',

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
        this.loadSharedWithMe();
    },

    switchShareTab(tab) {
        this._activeShareTab = tab;
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
            const shares = (await API.sharing.sharedByMe()) || [];
            if (!shares.length) {
                c.innerHTML = `<div class="shares-empty"><div class="shares-empty-icon">📤</div><p>${I18N.t('shares.empty_by_me')}</p></div>`;
                return;
            }
            // Group by file id
            const grouped = {};
            for (const s of shares) {
                if (!grouped[s.id]) grouped[s.id] = { file: s, recipients: [] };
                grouped[s.id].recipients.push({ name: s.shared_with_name, permission: s.permission });
            }
            let h = '<div class="file-grid">';
            for (const g of Object.values(grouped)) {
                const f = g.file;
                const icon = UI.fileIcon(f.name, false);
                const dbl = `UI.openFile(${f.id},${UI.escJson(f.name)},${f.size_bytes || 0})`;
                const names = g.recipients.map(r => r.name).filter(Boolean);
                const countText = names.length === 1 ? names[0] : I18N.tn('shares.recipients_count', names.length);
                h += `<div class="file-card" ondblclick="${dbl}">
                    <div class="file-card-icon document">${icon}</div>
                    <div class="file-card-name" title="${UI.esc(f.name)}">${UI.esc(f.name)}</div>
                    <div class="file-card-meta">${UI.formatBytes(f.size_bytes || 0)}</div>
                    <div class="file-card-badge">→ ${UI.esc(countText)}</div>
                    ${names.length > 1 ? `<div class="file-card-recipients" title="${UI.esc(names.join(', '))}">${names.map(n => UI.esc(n)).join(', ')}</div>` : ''}
                </div>`;
            }
            c.innerHTML = h + '</div>';
        } catch (err) {
            c.innerHTML = `<p class="text-muted">${UI.esc(err.message)}</p>`;
        }
    },

    async loadSharedWithMe() {
        const c = document.getElementById('shares-tab-content');
        if (!c) return;
        try {
            const files = (await API.sharing.sharedWithMe()) || [];
            if (!files.length) {
                c.innerHTML = `<div class="shares-empty"><div class="shares-empty-icon">📥</div><p>${I18N.t('shares.empty_with_me')}</p></div>`;
                return;
            }
            let h = '<div class="file-grid">';
            for (const f of files) {
                const icon = UI.fileIcon(f.name, false);
                const dbl = `UI.openFile(${f.id},${UI.escJson(f.name)},${f.size_bytes || 0})`;
                h += `<div class="file-card" ondblclick="${dbl}">
                    <div class="file-card-icon document">${icon}</div>
                    <div class="file-card-name" title="${UI.esc(f.name)}">${UI.esc(f.name)}</div>
                    <div class="file-card-meta">${UI.formatBytes(f.size_bytes || 0)}${f.owner_name ? ' · ' + UI.esc(f.owner_name) : ''}</div>
                    <div class="file-card-badge">${f.permission === 'edit' ? '✏️ ' + I18N.t('shares.can_edit') : '👁 ' + I18N.t('shares.view_only')}</div>
                </div>`;
            }
            c.innerHTML = h + '</div>';
        } catch (err) {
            c.innerHTML = `<p class="text-muted">${UI.esc(err.message)}</p>`;
        }
    },

    /* ── Share Modal (multi-user) ── */

    _currentFile: null,
    _pendingRecipients: [],
    _existingShareUserIds: [],
    _selectedVisibility: 'private',

    showShareModal(file) {
        const isAdmin = App.user && App.user.role === 'admin';
        const vis = file.visibility || 'private';
        let visibilityHTML = '';
        if (isAdmin) {
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
        UI.showModal(I18N.t('shares.modal_title', { name: file.name }), `
            <div class="form-group">
                <label>${I18N.t('shares.search_label')}</label>
                <input type="text" id="share-user-search" class="form-control" placeholder="${I18N.t('shares.search_placeholder')}" oninput="SharesPage.searchUsers(this.value)">
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
            <div id="share-existing"><p class="text-muted">${I18N.t('common.loading')}</p></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="SharesPage.saveShare()">${I18N.t('common.save')}</button>`);
        this._currentFile = file;
        this._pendingRecipients = [];
        this._existingShareUserIds = [];
        this._selectedVisibility = vis;
        this.loadExistingShares(file.id);
    },

    async searchUsers(q) {
        const r = document.getElementById('share-user-results');
        if (!r) return;
        if (!q || q.length < 2) { r.innerHTML = ''; return; }
        try {
            const users = await API.sharing.searchUsers(q);
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
                    <span class="share-user-avatar">${(u.full_name||'?').charAt(0).toUpperCase()}</span>
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
                <span class="share-user-avatar">${(p.name||'?').charAt(0).toUpperCase()}</span>
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
        if (!this._currentFile) return;
        try {
            // Save visibility
            const newVis = this._selectedVisibility || 'private';
            const oldVis = this._currentFile.visibility || 'private';
            if (newVis !== oldVis) {
                await API.sharing.setVisibility(this._currentFile.id, newVis);
                this._currentFile.visibility = newVis;
            }
            // Share with all pending recipients
            const errors = [];
            for (const p of this._pendingRecipients) {
                try {
                    await API.sharing.share(this._currentFile.id, p.id, p.permission);
                } catch (e) { errors.push(`${p.name}: ${e.message}`); }
            }
            UI.closeModal();
            if (errors.length) {
                UI.toast(I18N.t('shares.some_errors', { errors: errors.join('; ') }), 'error');
            } else if (this._pendingRecipients.length) {
                UI.toast(I18N.t('shares.shared_with_count', { count: this._pendingRecipients.length }), 'success');
            } else {
                UI.toast(I18N.t('shares.changes_saved'), 'success');
            }
            this._pendingRecipients = [];
            if (typeof FilesPage !== 'undefined') FilesPage.loadFiles();
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
                    <span class="share-user-avatar">${(s.full_name || '?').charAt(0).toUpperCase()}</span>
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
