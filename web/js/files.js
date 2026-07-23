/* Paylash — File Manager */
const FilesPage = {
    currentFolder: null,
    currentScope: 'personal',
    currentProjectId: null,
    currentProjectName: '',
    currentProjectPermission: null,
    viewMode: 'grid',
    breadcrumbs: [],
    files: [],
    folders: [],
    filesLimit: 50,
    filesOffset: 0,
    hasMoreFiles: false,
    // Selection keys are "file:123" / "folder:45" — prefixed so a file and a
    // folder that happen to share a numeric id can't collide in the Set.
    selected: new Set(),

    // Whether the current user may upload/create/rename/delete in the active scope.
    canManage() {
        const isAdmin = App.user && App.user.role === 'admin';
        if (isAdmin) return true;
        if (this.currentScope === 'personal' || this.currentScope === 'common') return true;
        if (this.currentScope === 'project') return this.currentProjectPermission === 'edit';
        return false;
    },

    render() {
        const canUpload = this.canManage();
        return `
        <div class="files-page">
            <div class="files-toolbar">
                <div class="files-toolbar-left">
                    <label class="select-all-check" title="${I18N.t('files.select_all')}">
                        <input type="checkbox" id="select-all-checkbox" onchange="FilesPage.toggleSelectAll(this.checked)">
                    </label>
                </div>
                <div class="files-toolbar-right">
                    <div class="search-box">
                        <span class="search-icon">${UI.icons.search}</span>
                        <input type="text" id="file-search" placeholder="${I18N.t('files.search_placeholder')}" oninput="FilesPage.onSearch(this.value)">
                    </div>
                    <button class="btn btn-icon btn-ghost ${this.viewMode === 'grid' ? 'active' : ''}" onclick="FilesPage.setView('grid')" title="${I18N.t('files.view_grid')}" aria-label="${I18N.t('files.view_grid')}">${UI.icons.grid}</button>
                    <button class="btn btn-icon btn-ghost ${this.viewMode === 'list' ? 'active' : ''}" onclick="FilesPage.setView('list')" title="${I18N.t('files.view_list')}" aria-label="${I18N.t('files.view_list')}">${UI.icons.list}</button>
                </div>
            </div>
            <div id="bulk-actions-bar" class="bulk-actions-bar hidden"></div>
            ${canUpload ? `<div class="files-actions">
                <button class="btn btn-primary btn-sm" onclick="FilesPage.showUploadModal()">${UI.icons.upload} ${I18N.t('files.upload_button')}</button>
                <button class="btn btn-ghost btn-sm" onclick="FilesPage.showUploadFolderModal()" title="${I18N.t('files.upload_folder_button')}">${UI.icons.folder} ${I18N.t('files.upload_folder_button')}</button>
                <button class="btn btn-ghost btn-sm" onclick="FilesPage.showNewFolderModal()">${UI.icons.plus} ${I18N.t('files.new_folder_button')}</button>
                <div class="new-file-dropdown">
                    <button class="btn btn-ghost btn-sm" onclick="FilesPage.toggleNewFileMenu(event)">${UI.icons.fileNew} ${I18N.t('files.new_file_button')}</button>
                    <div class="new-file-menu hidden" id="new-file-menu">
                        <div class="new-file-option" onclick="FilesPage.createNewFile('docx')">
                            <span class="new-file-option-icon docx-icon">📝</span>
                            <div class="new-file-option-info">
                                <span class="new-file-option-name">${I18N.t('files.new_word_doc')}</span>
                                <span class="new-file-option-ext">.docx</span>
                            </div>
                        </div>
                        <div class="new-file-option" onclick="FilesPage.createNewFile('xlsx')">
                            <span class="new-file-option-icon xlsx-icon">📊</span>
                            <div class="new-file-option-info">
                                <span class="new-file-option-name">${I18N.t('files.new_excel_doc')}</span>
                                <span class="new-file-option-ext">.xlsx</span>
                            </div>
                        </div>
                    </div>
                </div>
            </div>` : ''}
            <div class="breadcrumbs" id="breadcrumbs"></div>
            <input type="file" id="file-input" multiple style="display:none" onchange="FilesPage.handleFileSelect(this.files)">
            <input type="file" id="folder-input" webkitdirectory directory multiple style="display:none" onchange="FilesPage.handleFolderSelect(this.files)">
            <div id="upload-progress" class="upload-progress hidden"></div>
            <div id="files-content">${UI.skeletonCards(6)}</div>
            <div class="drop-overlay hidden" id="drop-overlay">
                <div class="drop-overlay-content">${UI.icons.upload}<p>${I18N.t('files.drop_files_here')}</p><p class="drop-overlay-hint">${I18N.t('files.drop_folders_hint')}</p></div>
            </div>
        </div>`;
    },

    async init() { await this.loadFiles(); this.initDragDrop(); },

    async loadFiles() {
        const c = document.getElementById('files-content');
        if (!c) return;
        c.innerHTML = UI.skeletonCards(6);
        this.filesOffset = 0;
        this.clearSelection();
        try {
            const p = { scope: this.currentScope, limit: this.filesLimit, offset: 0 };
            if (this.currentFolder) p.folder_id = this.currentFolder;
            if (this.currentScope === 'project' && this.currentProjectId) p.project_id = this.currentProjectId;
            const data = await API.files.list(p);
            this.files = data.files || [];
            this.folders = data.folders || [];
            this.breadcrumbs = data.breadcrumbs || [];
            this.hasMoreFiles = this.files.length === this.filesLimit;
            this.renderBreadcrumbs();
            this.renderFiles();
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>${I18N.t('files.load_failed')}</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    async loadMoreFiles() {
        const btn = document.getElementById('files-load-more');
        if (btn) { btn.disabled = true; btn.textContent = I18N.t('common.loading'); }
        try {
            this.filesOffset += this.filesLimit;
            const p = { scope: this.currentScope, limit: this.filesLimit, offset: this.filesOffset };
            if (this.currentFolder) p.folder_id = this.currentFolder;
            if (this.currentScope === 'project' && this.currentProjectId) p.project_id = this.currentProjectId;
            const data = await API.files.list(p);
            const more = data.files || [];
            this.files = this.files.concat(more);
            this.hasMoreFiles = more.length === this.filesLimit;
            this.renderFiles();
        } catch (err) {
            UI.toast(err.message, 'error');
        }
    },

    renderBreadcrumbs() {
        const el = document.getElementById('breadcrumbs');
        if (!el) return;
        const rootLabel = this.currentScope === 'personal' ? I18N.t('app.nav_personal') : this.currentScope === 'project' ? (this.currentProjectName || I18N.t('app.project_label')) : I18N.t('app.nav_common');
        let h = '';
        if (this.currentFolder) {
            // Ancestors as returned by the server are root-most first, with
            // the current folder itself appended last — so "one level up" is
            // whatever's second-to-last, or the scope root if there's only
            // the current folder in the list.
            const parentId = this.breadcrumbs.length > 1 ? this.breadcrumbs[this.breadcrumbs.length - 2].id : null;
            h += `<button class="btn btn-icon btn-ghost btn-sm breadcrumb-back" onclick="FilesPage.goToFolder(${parentId})" title="${I18N.t('files.back_button')}" aria-label="${I18N.t('files.back_button')}">${UI.icons.back}</button>`;
        }
        h += `<a class="breadcrumb-item" onclick="FilesPage.goToFolder(null)">${UI.esc(rootLabel)}</a>`;
        this.breadcrumbs.forEach((b, i) => {
            const isCurrent = i === this.breadcrumbs.length - 1;
            h += `<span class="breadcrumb-sep">/</span>`;
            h += isCurrent
                ? `<span class="breadcrumb-item breadcrumb-current">${UI.esc(b.name)}</span>`
                : `<a class="breadcrumb-item" onclick="FilesPage.goToFolder(${b.id})">${UI.esc(b.name)}</a>`;
        });
        el.innerHTML = h;
    },

    renderFiles() {
        const c = document.getElementById('files-content');
        if (!c) return;
        const items = [...this.folders.map(f => ({ ...f, isFolder: true })), ...this.files];
        if (!items.length) {
            c.innerHTML = `<div class="empty-state"><div class="empty-state-icon">📂</div><p>${I18N.t('files.empty_title')}</p><p class="text-muted">${I18N.t('files.empty_subtitle')}</p></div>`;
            return;
        }
        if (this.viewMode === 'grid') {
            c.innerHTML = '<div class="file-grid">' + items.map(i => this.gridCard(i)).join('') + '</div>';
        } else {
            c.innerHTML = `<div class="file-list">
                <div class="file-list-header"><div>${I18N.t('files.col_name')}</div><div>${I18N.t('files.col_size')}</div><div>${I18N.t('files.col_date')}</div><div></div></div>
                ${items.map(i => this.listRow(i)).join('')}
            </div>`;
        }
        if (this.hasMoreFiles) {
            c.innerHTML += `<div style="text-align:center;margin-top:12px">
                <button class="btn btn-ghost btn-sm" id="files-load-more" onclick="FilesPage.loadMoreFiles()">${I18N.t('files.load_more')}</button>
            </div>`;
        }
        this.syncSelectionUI();
    },

    /* ── Multi-select ── */

    selectionKey(item) { return (item.isFolder ? 'folder:' : 'file:') + item.id; },

    toggleSelect(key, ev) {
        if (ev) ev.stopPropagation();
        if (this.selected.has(key)) this.selected.delete(key); else this.selected.add(key);
        this.renderFiles();
    },

    toggleSelectAll(checked) {
        this.selected.clear();
        if (checked) {
            for (const f of this.folders) this.selected.add('folder:' + f.id);
            for (const f of this.files) this.selected.add('file:' + f.id);
        }
        this.renderFiles();
    },

    clearSelection() {
        if (!this.selected.size) { this.syncSelectionUI(); return; }
        this.selected.clear();
        this.renderFiles();
    },

    selectedItems() {
        const items = [];
        for (const key of this.selected) {
            const [kind, idStr] = key.split(':');
            const id = parseInt(idStr, 10);
            const src = kind === 'folder' ? this.folders : this.files;
            const found = src.find(x => x.id === id);
            if (found) items.push({ ...found, isFolder: kind === 'folder' });
        }
        return items;
    },

    // Keeps the toolbar's "select all" checkbox and the bulk action bar in
    // sync with `selected` — called after every render/render (renderFiles)
    // rather than threaded through each individual mutation, so it can never
    // drift out of sync with what's actually on screen.
    syncSelectionUI() {
        const selAll = document.getElementById('select-all-checkbox');
        if (selAll) {
            const total = this.files.length + this.folders.length;
            selAll.checked = total > 0 && this.selected.size === total;
            selAll.indeterminate = this.selected.size > 0 && this.selected.size < total;
        }
        this.renderBulkBar();
    },

    renderBulkBar() {
        const bar = document.getElementById('bulk-actions-bar');
        if (!bar) return;
        if (!this.selected.size) { bar.classList.add('hidden'); bar.innerHTML = ''; return; }
        const items = this.selectedItems();
        const canManage = this.canManage();
        const allFiles = items.every(i => !i.isFolder);
        bar.classList.remove('hidden');
        bar.innerHTML = `
            <span class="bulk-bar-count">${I18N.tn('files.selected_count', items.length)}</span>
            <div class="bulk-bar-actions">
                <button class="btn btn-ghost btn-sm" onclick="FilesPage.bulkDownload()">${UI.icons.download} ${I18N.t('files.action_download')}</button>
                ${canManage && allFiles ? `<button class="btn btn-ghost btn-sm" onclick="FilesPage.bulkShare()">${UI.icons.share} ${I18N.t('files.action_share')}</button>` : ''}
                ${canManage ? `<button class="btn btn-ghost btn-sm" onclick="FilesPage.bulkMove()">${UI.icons.folder} ${I18N.t('files.action_move')}</button>` : ''}
                ${canManage ? `<button class="btn btn-ghost btn-sm btn-danger" onclick="FilesPage.bulkDelete()">${UI.icons.trash} ${I18N.t('files.action_delete')}</button>` : ''}
                <button class="btn btn-icon btn-ghost btn-sm" onclick="FilesPage.clearSelection()" title="${I18N.t('files.clear_selection')}" aria-label="${I18N.t('files.clear_selection')}">✕</button>
            </div>`;
    },

    // A single combined zip instead of one browser download per selected
    // item — much nicer once more than a couple of files are selected, and
    // for a single lone file, skips the zip step entirely and just downloads
    // it directly (no reason to zip one file).
    bulkDownload() {
        const items = this.selectedItems();
        if (items.length === 1) {
            const only = items[0];
            if (only.isFolder) this.downloadFolder(only.id, only.name);
            else this.download(only.id, only.name);
            return;
        }
        const fileIds = items.filter(i => !i.isFolder).map(i => i.id);
        const folderIds = items.filter(i => i.isFolder).map(i => i.id);
        UI.toast(I18N.t('files.bulk_zip_preparing'), 'info');
        const a = document.createElement('a');
        a.href = API.files.bulkDownloadURL(fileIds, folderIds);
        a.download = 'paylash-files.zip';
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
    },

    bulkShare() {
        const items = this.selectedItems().filter(i => !i.isFolder);
        if (items.length) SharesPage.showShareModal(items);
    },

    async bulkMove() {
        const items = this.selectedItems();
        if (!items.length) return;
        try {
            const folders = await API.folders.tree(this.currentScope, this.currentProjectId);
            // Exclude every selected folder and all of its descendants —
            // moving a folder into its own subtree would orphan it.
            const excluded = new Set(items.filter(i => i.isFolder).map(i => i.id));
            const byParent = {};
            for (const f of folders) { const k = f.parent_id || 'root'; (byParent[k] = byParent[k] || []).push(f); }
            const stack = items.filter(i => i.isFolder).map(i => i.id);
            while (stack.length) {
                const cur = stack.pop();
                for (const f of (byParent[cur] || [])) { excluded.add(f.id); stack.push(f.id); }
            }
            const lines = UI.flattenFolderTree(folders).filter(l => !excluded.has(l.id));
            const options = [`<option value="">${I18N.t('common.root_option')}</option>`]
                .concat(lines.map(l => `<option value="${l.id}">${'— '.repeat(l.depth)}${UI.esc(l.name)}</option>`));
            UI.showModal(I18N.t('files.bulk_move_title', { count: items.length }),
                `<div class="form-group"><label>${I18N.t('files.move_target_label')}</label><select id="move-target" class="form-control">${options.join('')}</select></div>`,
                `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doBulkMove()">${I18N.t('common.move')}</button>`);
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    // BULK_CONCURRENCY caps how many move/delete/share requests for a
    // multi-select batch are ever in flight at once — high enough that a
    // 50-item selection doesn't wait on 50 sequential round-trips, low
    // enough not to hammer the server with one request per item at once.
    BULK_CONCURRENCY: 4,

    async doBulkMove() {
        const val = document.getElementById('move-target').value;
        const targetId = val ? parseInt(val) : null;
        const items = this.selectedItems();
        const failures = await UI.runPooled(items, this.BULK_CONCURRENCY, async item => {
            if (item.isFolder) await API.folders.move(item.id, targetId);
            else await API.files.move(item.id, targetId);
        });
        UI.closeModal();
        if (failures.length) UI.toast(I18N.t('shares.some_errors', { errors: failures.map(f => `${f.item.name}: ${f.error.message}`).join('; ') }), 'error');
        else UI.toast(I18N.t('files.move_done'), 'success');
        this.clearSelection();
        this.loadFiles();
    },

    bulkDelete() {
        const items = this.selectedItems();
        if (!items.length) return;
        UI.confirmAction(I18N.t('files.bulk_delete_title'),
            I18N.t('files.bulk_delete_body', { count: items.length }),
            I18N.t('common.delete'), async () => {
                const failures = await UI.runPooled(items, this.BULK_CONCURRENCY, async item => {
                    if (item.isFolder) await API.folders.delete(item.id);
                    else await API.files.delete(item.id);
                });
                if (failures.length) UI.toast(I18N.t('shares.some_errors', { errors: failures.map(f => `${f.item.name}: ${f.error.message}`).join('; ') }), 'error');
                else UI.toast(I18N.t('files.bulk_delete_done'), 'success');
                this.clearSelection();
                this.loadFiles();
                App.loadStorageUsage();
            });
    },

    gridCard(item) {
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const dbl = item.isFolder ? `FilesPage.goToFolder(${item.id})` : `UI.openFile(${item.id},${UI.escJson(item.name)},${item.size_bytes || 0})`;
        const itemJson = UI.escJson(item);
        const key = this.selectionKey(item);
        const isSelected = this.selected.has(key);
        const ext = item.isFolder ? '' : item.name.split('.').pop().toLowerCase();
        // Small cached JPEG preview (see FileThumbnail server-side) instead of
        // the full original — a folder full of camera photos used to fire off
        // dozens of concurrent full-resolution downloads just to paint the
        // grid. Only formats the server can actually generate a preview for
        // (see isThumbnailableImage in internal/api/files.go) go through
        // /thumbnail; the handful of image types it can't decode (svg, webp,
        // ico, bmp, tiff) keep using /download directly since those are
        // either already tiny (svg/ico) or too rare here to be worth a
        // second code path. The file's version is in the URL so the browser
        // can cache the response forever and still pick up re-uploads/edits.
        const iconHtml = !item.isFolder && UI.isThumbnailable(ext)
            ? `<img class="file-card-thumb" src="/api/files/${item.id}/thumbnail?v=${item.version || 0}" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
            : !item.isFolder && UI.isImage(ext)
            ? `<img class="file-card-thumb" src="/api/files/${item.id}/download" loading="lazy" alt="" onerror="FilesPage.thumbError(this)">`
            : `<div class="file-card-icon ${cls}">${UI.fileIcon(item.name, item.isFolder)}</div>`;
        return `<div class="file-card${isSelected ? ' selected' : ''}" tabindex="0" role="button" aria-label="${UI.esc(item.name)}" ondblclick="${dbl}" oncontextmenu="FilesPage.showMenu(event,${itemJson})" onkeydown="FilesPage.onCardKeydown(event,${itemJson})">
            <label class="file-card-select" onclick="event.stopPropagation()" ondblclick="event.stopPropagation()">
                <input type="checkbox" ${isSelected ? 'checked' : ''} onchange="FilesPage.toggleSelect('${key}',event)" aria-label="${I18N.t('files.select_item')}">
            </label>
            ${iconHtml}
            <div class="file-card-name" title="${UI.esc(item.name)}">${UI.esc(item.name)}</div>
            ${!item.isFolder ? `<div class="file-card-meta">${UI.formatBytes(item.size_bytes || 0)} · ${UI.formatDate(item.updated_at || item.created_at)}</div>` : `<div class="file-card-meta">${I18N.t('files.folder_label')}</div>`}
        </div>`;
    },

    // Falls back to the generic file icon if a thumbnail fails to load
    // (e.g. the image was deleted between listing and rendering).
    thumbError(img) { img.outerHTML = '<div class="file-card-icon image">🖼</div>'; },

    // Grid cards are focusable (tabindex="0", see gridCard) but previously
    // had no keyboard interaction at all — a mouse was the only way to open,
    // select, or reach the context menu for anything in grid view. Enter
    // opens (same as a double-click), Space toggles selection, and the
    // "ContextMenu" key / Shift+F10 opens the same menu a right-click gets.
    // Ignored when the key came from an inner focusable control (the
    // checkbox) so this doesn't double-handle keys that element already owns.
    onCardKeydown(ev, item) {
        if (ev.target !== ev.currentTarget) return;
        if (ev.key === 'Enter') {
            ev.preventDefault();
            if (item.isFolder) this.goToFolder(item.id);
            else UI.openFile(item.id, item.name, item.size_bytes || 0);
        } else if (ev.key === ' ') {
            ev.preventDefault();
            this.toggleSelect(this.selectionKey(item));
        } else if (ev.key === 'ContextMenu' || (ev.shiftKey && ev.key === 'F10')) {
            ev.preventDefault();
            this.showMenu(ev, item);
        }
    },

    listRow(item) {
        const icon = UI.fileIcon(item.name, item.isFolder);
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const dbl = item.isFolder ? `FilesPage.goToFolder(${item.id})` : `UI.openFile(${item.id},${UI.escJson(item.name)},${item.size_bytes || 0})`;
        const itemJson = UI.escJson(item);
        const key = this.selectionKey(item);
        const isSelected = this.selected.has(key);
        return `<div class="file-list-row${isSelected ? ' selected' : ''}" ondblclick="${dbl}" oncontextmenu="FilesPage.showMenu(event,${itemJson})">
            <div class="file-list-name">
                <label class="file-list-select" onclick="event.stopPropagation()" ondblclick="event.stopPropagation()">
                    <input type="checkbox" ${isSelected ? 'checked' : ''} onchange="FilesPage.toggleSelect('${key}',event)" aria-label="${I18N.t('files.select_item')}">
                </label>
                <span class="file-list-icon ${cls}">${icon}</span>${UI.esc(item.name)}
            </div>
            <div class="file-list-size">${item.isFolder ? '—' : UI.formatBytes(item.size_bytes || 0)}</div>
            <div class="file-list-date">${UI.formatDate(item.updated_at || item.created_at)}</div>
            <div class="file-list-actions"><button class="btn btn-icon btn-sm" onclick="FilesPage.showMenu(event,${itemJson})" aria-label="${I18N.t('common.actions')}">⋮</button></div>
        </div>`;
    },

    showMenu(e, item) {
        e.preventDefault(); e.stopPropagation();
        const canManage = this.canManage();
        const items = [];
        if (item.isFolder) {
            items.push({ action: 'open', label: I18N.t('files.action_open'), icon: '📂', handler: () => this.goToFolder(item.id) });
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => this.downloadFolder(item.id, item.name) });
            if (canManage) {
                items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.renameFolder(item) });
                items.push({ action: 'move', label: I18N.t('files.action_move'), icon: '📁', handler: () => this.moveItem(item) });
                items.push({ divider: true });
                items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.deleteFolder(item) });
            }
        } else {
            if (UI.isMediaPreviewable(item.name)) items.push({ action: 'preview', label: I18N.t('files.action_preview'), icon: '👁', handler: () => PreviewPage.open(item.id, item.name, item.size_bytes) });
            else if (UI.isCollaboraEditable(item.name)) items.push({ action: 'edit', label: I18N.t('files.action_edit'), icon: '📝', handler: () => EditorPage.open(item.id, item.name) });
            else if (UI.isCollaboraViewable(item.name)) items.push({ action: 'view', label: I18N.t('files.action_preview'), icon: '👁', handler: () => EditorPage.open(item.id, item.name) });
            items.push({ action: 'download', label: I18N.t('files.action_download'), icon: '📥', handler: () => this.download(item.id, item.name) });
            items.push({ action: 'versions', label: I18N.t('files.action_versions'), icon: '🕓', handler: () => this.showVersionsModal(item) });
            if (canManage) {
                items.push({ action: 'share', label: I18N.t('files.action_share'), icon: '🔗', handler: () => SharesPage.showShareModal(item) });
                items.push({ action: 'rename', label: I18N.t('files.action_rename'), icon: '✏️', handler: () => this.renameFile(item) });
                items.push({ action: 'move', label: I18N.t('files.action_move'), icon: '📁', handler: () => this.moveItem(item) });
                items.push({ divider: true });
                items.push({ action: 'delete', label: I18N.t('files.action_delete'), icon: '🗑', danger: true, handler: () => this.deleteFile(item) });
            }
        }
        const [x, y] = UI.eventPos(e);
        UI.showContextMenu(x, y, items);
    },

    async showVersionsModal(item) {
        UI.showModal(I18N.t('files.versions_title'), '<div class="text-muted" style="text-align:center;padding:20px"><div class="spinner"></div></div>', '');
        try {
            const versions = await API.files.versions(item.id);
            const body = !versions || !versions.length
                ? `<p class="text-muted">${I18N.t('files.versions_none')}</p>`
                : `<div class="version-list">${versions.map(v => `
                    <div class="version-item">
                        <div class="version-item-info">
                            <div>${v.is_latest ? `<strong>${I18N.t('files.version_current')}</strong>` : UI.formatDate(v.last_modified)}</div>
                            <div class="text-muted" style="font-size:.78rem">${UI.formatBytes(v.size_bytes)}${v.is_latest ? '' : ' · ' + new Date(v.last_modified).toLocaleString(I18N.dateLocale())}</div>
                        </div>
                        <div style="display:flex;gap:6px">
                            <button class="btn btn-sm btn-ghost" onclick="API.files.downloadVersion(${item.id},'${v.version_id}')" title="${I18N.t('files.action_download')}" aria-label="${I18N.t('files.action_download')}">${UI.icons.download}</button>
                            ${!v.is_latest ? `<button class="btn btn-sm btn-ghost" onclick="FilesPage.restoreVersion(${item.id},'${v.version_id}')">${I18N.t('files.version_restore')}</button>` : ''}
                        </div>
                    </div>`).join('')}</div>`;
            UI.showModal(I18N.t('files.versions_title') + ' — ' + item.name, body, `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.close')}</button>`);
        } catch (e) {
            UI.showModal(I18N.t('files.versions_title'), `<p class="text-muted">${UI.esc(e.message)}</p>`, `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.close')}</button>`);
        }
    },

    async restoreVersion(id, versionId) {
        try {
            await API.files.restoreVersion(id, versionId);
            UI.toast(I18N.t('files.version_restored'), 'success');
            UI.closeModal();
            this.loadFiles();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    setScope(s, projectId, projectName, permission) {
        this.currentScope = s;
        this.currentProjectId = s === 'project' ? projectId : null;
        this.currentProjectName = s === 'project' ? (projectName || '') : '';
        this.currentProjectPermission = s === 'project' ? (permission || 'view') : null;
        this.currentFolder = null;
        App.renderPage('files');
    },
    setView(m) { this.viewMode = m; this.renderFiles(); },
    goToFolder(id) { this.currentFolder = id; this.loadFiles(); },

    _searchTimer: null,
    async onSearch(q) {
        clearTimeout(this._searchTimer);
        this._searchTimer = setTimeout(() => this._runSearch(q), 300);
    },
    async _runSearch(q) {
        if (!q || q.length < 2) { this.loadFiles(); return; }
        this.selected.clear();
        try { const data = await API.files.search(q); this.files = data || []; this.folders = []; this.breadcrumbs = []; this.renderBreadcrumbs(); this.renderFiles(); } catch {}
    },

    showUploadModal() { document.getElementById('file-input').click(); },
    showUploadFolderModal() { document.getElementById('folder-input').click(); },

    async handleFileSelect(fileList) {
        if (!fileList.length) return;
        const prog = document.getElementById('upload-progress');
        prog.classList.remove('hidden');
        for (const file of fileList) {
            const id = 'u-' + Math.random().toString(36).substr(2, 6);
            const isLarge = typeof Uploader !== 'undefined' && Uploader.isLarge(file);
            const resumeBadge = isLarge
                ? `<span class="upload-item-badge" title="${I18N.t('files.upload_resume_hint')}">${I18N.t('files.upload_resume_badge')}</span>`
                : '';
            prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.name)} ${resumeBadge}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
            try {
                const onProgress = pct => {
                    const f = document.getElementById(id + '-f'), p = document.getElementById(id + '-p');
                    if (f) f.style.width = pct + '%'; if (p) p.textContent = Math.round(pct) + '%';
                };
                // Large files (CAD/renders/scans) go through the resumable
                // direct-to-MinIO uploader instead of the single-shot XHR —
                // see web/js/upload.js for why. Uploader falls back to the
                // simple path itself if the resumable one isn't available.
                if (isLarge) {
                    await Uploader.uploadLarge(file, this.currentScope, this.currentFolder, this.currentProjectId, onProgress);
                } else {
                    await API.files.upload(file, this.currentScope, this.currentFolder, this.currentProjectId, onProgress);
                }
                document.getElementById(id)?.classList.add('upload-done');
            } catch (err) {
                UI.toast(I18N.t('files.upload_item_failed', { name: file.name, error: err.message }), 'error');
                document.getElementById(id)?.classList.add('upload-error');
            }
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadFiles(); App.loadStorageUsage();
        document.getElementById('file-input').value = '';
    },

    // Uploads a batch of {file, relativePath} entries (from the folder
    // <input> or a folder dropped onto the page), recreating the folder
    // structure server-side first via Uploader.resolveFolderPaths, then
    // reusing the normal per-file upload pipeline above.
    async handleFolderUpload(entries) {
        if (!entries.length) return;
        const prog = document.getElementById('upload-progress');
        prog.classList.remove('hidden');
        try {
            const resolved = await Uploader.resolveFolderPaths(entries, this.currentScope, this.currentFolder, this.currentProjectId);
            for (const { file, folderId } of resolved) {
                const id = 'u-' + Math.random().toString(36).substr(2, 6);
                const isLarge = typeof Uploader !== 'undefined' && Uploader.isLarge(file);
                const resumeBadge = isLarge
                    ? `<span class="upload-item-badge" title="${I18N.t('files.upload_resume_hint')}">${I18N.t('files.upload_resume_badge')}</span>`
                    : '';
                prog.innerHTML += `<div class="upload-item" id="${id}"><div class="upload-item-name">${UI.esc(file.webkitRelativePath || file.name)} ${resumeBadge}</div><div class="upload-item-bar"><div class="upload-item-fill" id="${id}-f"></div></div><div class="upload-item-pct" id="${id}-p">0%</div></div>`;
                try {
                    const onProgress = pct => {
                        const f = document.getElementById(id + '-f'), p = document.getElementById(id + '-p');
                        if (f) f.style.width = pct + '%'; if (p) p.textContent = Math.round(pct) + '%';
                    };
                    if (isLarge) {
                        await Uploader.uploadLarge(file, this.currentScope, folderId, this.currentProjectId, onProgress);
                    } else {
                        await API.files.upload(file, this.currentScope, folderId, this.currentProjectId, onProgress);
                    }
                    document.getElementById(id)?.classList.add('upload-done');
                } catch (err) {
                    UI.toast(I18N.t('files.upload_item_failed', { name: file.name, error: err.message }), 'error');
                    document.getElementById(id)?.classList.add('upload-error');
                }
            }
        } catch (err) {
            UI.toast(err.message, 'error');
        }
        setTimeout(() => { prog.innerHTML = ''; prog.classList.add('hidden'); }, 2000);
        this.loadFiles(); App.loadStorageUsage();
    },

    handleFolderSelect(fileList) {
        const entries = Array.from(fileList).map(file => ({ file, relativePath: file.webkitRelativePath || file.name }));
        this.handleFolderUpload(entries);
        document.getElementById('folder-input').value = '';
    },

    // Guarded per-DOM-node (pg.dataset.dragInited), not a page-singleton
    // flag: renderShell/renderPage swap #page-content's innerHTML on every
    // navigation, so a fresh .files-page element exists each time this runs.
    // A singleton flag used to survive that swap (only setScope() reset it),
    // so navigating Files -> Shared/Trash/Admin -> Files via the plain
    // sidebar link left the new element with no listeners at all — drag-and-
    // drop silently stopped working for the rest of the session.
    initDragDrop() {
        if (!this.canManage()) return;
        const pg = document.querySelector('.files-page');
        const ov = document.getElementById('drop-overlay');
        if (!pg || !ov || pg.dataset.dragInited) return;
        pg.dataset.dragInited = '1';
        let dragCounter = 0;
        pg.addEventListener('dragenter', ev => { ev.preventDefault(); dragCounter++; ov.classList.remove('hidden'); });
        pg.addEventListener('dragover', ev => { ev.preventDefault(); });
        pg.addEventListener('dragleave', ev => { ev.preventDefault(); dragCounter--; if (dragCounter <= 0) { dragCounter = 0; ov.classList.add('hidden'); } });
        pg.addEventListener('drop', ev => {
            ev.preventDefault(); dragCounter = 0; ov.classList.add('hidden');
            this.handleDrop(ev.dataTransfer);
        });
    },

    // Dropped items can be a flat batch of files or include whole folders —
    // DataTransferItemList exposes webkitGetAsEntry() for the latter, which
    // plain dataTransfer.files can't express (it flattens everything and
    // drops folder structure entirely). Falls back to the simple flat-file
    // path when nothing dropped is a directory (the common case, and cheap
    // to detect up front).
    async handleDrop(dataTransfer) {
        const items = dataTransfer.items;
        if (!items || !items.length || typeof items[0].webkitGetAsEntry !== 'function') {
            if (dataTransfer.files.length) this.handleFileSelect(dataTransfer.files);
            return;
        }
        const entries = [];
        let hasDirectory = false;
        for (const item of items) {
            const entry = item.webkitGetAsEntry && item.webkitGetAsEntry();
            if (!entry) continue;
            if (entry.isDirectory) hasDirectory = true;
            entries.push(entry);
        }
        if (!hasDirectory) {
            if (dataTransfer.files.length) this.handleFileSelect(dataTransfer.files);
            return;
        }
        const collected = [];
        await Promise.all(entries.map(entry => this._walkEntry(entry, '', collected)));
        this.handleFolderUpload(collected);
    },

    // Recursively walks a FileSystemEntry (file or directory), collecting
    // {file, relativePath} pairs — the same shape handleFolderSelect
    // produces from a <input webkitdirectory> selection, so both entry
    // points feed the same upload pipeline.
    _walkEntry(entry, basePath, out) {
        return new Promise((resolve, reject) => {
            if (entry.isFile) {
                entry.file(file => { out.push({ file, relativePath: basePath + entry.name }); resolve(); }, reject);
                return;
            }
            const reader = entry.createReader();
            const allEntries = [];
            const readBatch = () => {
                reader.readEntries(batch => {
                    if (!batch.length) {
                        Promise.all(allEntries.map(e => this._walkEntry(e, basePath + entry.name + '/', out))).then(resolve, reject);
                        return;
                    }
                    allEntries.push(...batch);
                    readBatch();
                }, reject);
            };
            readBatch();
        });
    },

    // A same-origin <a download> click lets the browser's own download
    // manager stream the response straight to disk — unlike the fetch()+blob
    // approach this replaced, which buffered the *entire* file in page
    // memory first. That was fine for small documents but would have made
    // downloading a 100GB scan/render either crawl or crash the tab. The app
    // server itself streams the object straight from MinIO without
    // buffering it either (see DownloadFile/http.ServeContent server-side),
    // so this stays cheap end-to-end regardless of file size.
    download(id, name) {
        const a = document.createElement('a');
        a.href = `/api/files/${id}/download`;
        a.download = name;
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
    },

    downloadFolder(id, name) {
        UI.toast(I18N.t('files.folder_zip_preparing', { name }), 'info');
        const a = document.createElement('a');
        a.href = API.folders.downloadURL(id);
        a.download = name + '.zip';
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
    },

    renameFile(item) {
        UI.showModal(I18N.t('files.rename_file_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="rename-input" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doRenameFile(${item.id})">${I18N.t('common.rename')}</button>`);
        setTimeout(() => { const i = document.getElementById('rename-input'); if (i) { i.focus(); i.select(); } }, 100);
    },
    async doRenameFile(id) {
        const n = document.getElementById('rename-input').value.trim(); if (!n) return;
        try { await API.files.rename(id, n); UI.closeModal(); UI.toast(I18N.t('files.rename_done'), 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    renameFolder(item) {
        UI.showModal(I18N.t('files.rename_folder_title'), `<div class="form-group"><label>${I18N.t('common.new_name_label')}</label><input type="text" id="rename-input" value="${UI.esc(item.name)}" class="form-control"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doRenameFolder(${item.id})">${I18N.t('common.rename')}</button>`);
        setTimeout(() => { const i = document.getElementById('rename-input'); if (i) { i.focus(); i.select(); } }, 100);
    },
    async doRenameFolder(id) {
        const n = document.getElementById('rename-input').value.trim(); if (!n) return;
        try { await API.folders.rename(id, n); UI.closeModal(); UI.toast(I18N.t('files.rename_done'), 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    async moveItem(item) {
        try {
            const folders = await API.folders.tree(this.currentScope, this.currentProjectId);
            const excluded = new Set();
            if (item.isFolder) {
                excluded.add(item.id);
                const byParent = {};
                for (const f of folders) { const k = f.parent_id || 'root'; (byParent[k] = byParent[k] || []).push(f); }
                const stack = [item.id];
                while (stack.length) {
                    const cur = stack.pop();
                    for (const f of (byParent[cur] || [])) { excluded.add(f.id); stack.push(f.id); }
                }
            }
            const lines = UI.flattenFolderTree(folders).filter(l => !excluded.has(l.id));
            const options = [`<option value="">${I18N.t('common.root_option')}</option>`]
                .concat(lines.map(l => `<option value="${l.id}">${'— '.repeat(l.depth)}${UI.esc(l.name)}</option>`));
            UI.showModal(I18N.t('files.move_title', { name: item.name }),
                `<div class="form-group"><label>${I18N.t('files.move_target_label')}</label><select id="move-target" class="form-control">${options.join('')}</select></div>`,
                `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doMove(${item.id},${!!item.isFolder})">${I18N.t('common.move')}</button>`);
        } catch (e) { UI.toast(e.message, 'error'); }
    },
    async doMove(id, isFolder) {
        const val = document.getElementById('move-target').value;
        const targetId = val ? parseInt(val) : null;
        try {
            if (isFolder) await API.folders.move(id, targetId);
            else await API.files.move(id, targetId);
            UI.closeModal();
            UI.toast(I18N.t('files.move_done'), 'success');
            this.loadFiles();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    deleteFile(item) {
        UI.confirmAction(I18N.t('files.delete_file_title'), I18N.t('files.delete_file_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.files.delete(item.id); UI.toast(I18N.t('files.delete_file_done'), 'success'); this.loadFiles(); App.loadStorageUsage(); }
            catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    deleteFolder(item) {
        UI.confirmAction(I18N.t('files.delete_folder_title'), I18N.t('files.delete_folder_body', { name: UI.esc(item.name) }), I18N.t('common.delete'), async () => {
            try { await API.folders.delete(item.id); UI.toast(I18N.t('files.delete_folder_done'), 'success'); this.loadFiles(); }
            catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    showNewFolderModal() {
        UI.showModal(I18N.t('files.new_folder_title'), `<div class="form-group"><label>${I18N.t('files.new_folder_name_label')}</label><input type="text" id="new-folder-name" class="form-control" placeholder="${I18N.t('files.new_folder_name_placeholder')}"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doCreateFolder()">${I18N.t('common.create')}</button>`);
        setTimeout(() => { const i = document.getElementById('new-folder-name'); if (i) i.focus(); }, 100);
    },
    async doCreateFolder() {
        const n = document.getElementById('new-folder-name').value.trim(); if (!n) return;
        try { await API.folders.create(n, this.currentScope, this.currentFolder, this.currentProjectId); UI.closeModal(); UI.toast(I18N.t('files.folder_created'), 'success'); this.loadFiles(); } catch (e) { UI.toast(e.message, 'error'); }
    },

    toggleNewFileMenu(e) {
        e.stopPropagation();
        const menu = document.getElementById('new-file-menu');
        if (!menu) return;
        if (!menu.classList.contains('hidden')) { this.closeNewFileMenu(); return; }
        menu.classList.remove('hidden');
        // Bound to `this` and stashed so closeNewFileMenu can remove exactly
        // this listener however the menu ends up closing (outside click, or
        // createNewFile hiding it directly after picking an option) —
        // otherwise the latter path leaves a stale document listener behind
        // every time, one more each time the menu is reopened.
        this._newFileMenuCloseHandler = (ev) => {
            if (!menu.contains(ev.target)) this.closeNewFileMenu();
        };
        setTimeout(() => document.addEventListener('click', this._newFileMenuCloseHandler), 0);
    },

    closeNewFileMenu() {
        document.getElementById('new-file-menu')?.classList.add('hidden');
        if (this._newFileMenuCloseHandler) {
            document.removeEventListener('click', this._newFileMenuCloseHandler);
            this._newFileMenuCloseHandler = null;
        }
    },

    createNewFile(type) {
        this.closeNewFileMenu();
        const defaults = { docx: I18N.t('files.new_file_default_doc'), xlsx: I18N.t('files.new_file_default_sheet') };
        const defaultName = defaults[type] || I18N.t('files.new_file_default_doc');
        UI.showModal(I18N.t('files.new_file_title'),
            `<div class="form-group"><label>${I18N.t('files.new_file_name_label')}</label><input type="text" id="new-file-name" class="form-control" placeholder="${UI.esc(defaultName)}" value="${UI.esc(defaultName)}"></div>
             <div class="new-file-type-badge"><span class="file-type-label">${type.toUpperCase()}</span></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="FilesPage.doCreateNewFile('${type}')">${I18N.t('common.create')}</button>`);
        setTimeout(() => { const i = document.getElementById('new-file-name'); if (i) { i.focus(); i.select(); } }, 100);
    },

    async doCreateNewFile(type) {
        const name = document.getElementById('new-file-name')?.value.trim();
        if (!name) { UI.toast(I18N.t('files.file_name_required'), 'error'); return; }
        try {
            const file = await API.files.createBlank(name, type, this.currentScope, this.currentFolder, this.currentProjectId);
            UI.closeModal();
            UI.toast(I18N.t('files.file_created'), 'success');
            this.loadFiles();
            App.loadStorageUsage();
            // Open the file in editor if it's an editable document
            if (type === 'docx' || type === 'xlsx') {
                setTimeout(() => EditorPage.open(file.id, file.name), 500);
            }
        } catch (e) { UI.toast(e.message || I18N.t('files.file_create_failed'), 'error'); }
    },

};
