/* Paylash — Trash (soft-deleted files/folders) */
const TrashPage = {
    items: [],

    render() {
        return `
        <div class="files-page">
            <div class="files-toolbar">
                <div class="files-toolbar-left">
                    <p class="text-muted" style="font-size:.8rem">${I18N.t('trash.auto_purge_notice')}</p>
                </div>
                <div class="files-toolbar-right">
                    <button class="btn btn-danger btn-sm" onclick="TrashPage.confirmEmpty()">${UI.icons.trash} ${I18N.t('trash.empty_button')}</button>
                </div>
            </div>
            <div id="trash-content">${UI.skeletonCards(6)}</div>
        </div>`;
    },

    async init() { await this.load(); },

    async load() {
        const c = document.getElementById('trash-content');
        if (!c) return;
        c.innerHTML = UI.skeletonCards(6);
        try {
            const data = await API.trash.list();
            this.items = [
                ...(data.folders || []).map(f => ({ ...f, isFolder: true })),
                ...(data.files || []),
            ];
            this.renderItems();
        } catch (err) {
            c.innerHTML = `<div class="empty-state"><p>${I18N.t('trash.load_failed')}</p><p class="text-muted">${UI.esc(err.message)}</p></div>`;
        }
    },

    renderItems() {
        const c = document.getElementById('trash-content');
        if (!c) return;
        if (!this.items.length) {
            c.innerHTML = `<div class="empty-state"><div class="empty-state-icon">🗑</div><p>${I18N.t('trash.empty_state')}</p></div>`;
            return;
        }
        c.innerHTML = '<div class="file-grid">' + this.items.map(i => this.card(i)).join('') + '</div>';
    },

    card(item) {
        const icon = UI.fileIcon(item.name, item.isFolder);
        const cls = UI.fileIconClass(item.name, item.isFolder);
        const isFolder = item.isFolder ? 'true' : 'false';
        return `<div class="file-card">
            <div class="file-card-icon ${cls}">${icon}</div>
            <div class="file-card-name" title="${UI.esc(item.name)}">${UI.esc(item.name)}</div>
            <div class="file-card-meta">${I18N.t('trash.deleted_suffix', { date: UI.formatDate(item.deleted_at) })}</div>
            <div style="display:flex;gap:6px;margin-top:8px">
                <button class="btn btn-sm btn-ghost" style="flex:1" onclick="TrashPage.restore(${isFolder},${item.id})" title="${I18N.t('trash.restore_title')}">↩ ${I18N.t('trash.restore_title')}</button>
                <button class="btn btn-sm btn-danger" onclick="TrashPage.confirmPurge(${isFolder},${item.id},${UI.escJson(item.name)})" title="${I18N.t('trash.purge_title')}" aria-label="${I18N.t('trash.purge_title')}">🗑</button>
            </div>
        </div>`;
    },

    async restore(isFolder, id) {
        try {
            if (isFolder) await API.trash.restoreFolder(id);
            else await API.trash.restoreFile(id);
            UI.toast(I18N.t('trash.restored'), 'success');
            this.load();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    confirmPurge(isFolder, id, name) {
        UI.confirmAction(I18N.t('trash.purge_confirm_title'), I18N.t('trash.purge_confirm_body', { name: UI.esc(name) }), I18N.t('common.delete'), async () => {
            try {
                if (isFolder) await API.trash.purgeFolder(id);
                else await API.trash.purgeFile(id);
                UI.toast(I18N.t('trash.purged'), 'success');
                this.load();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    confirmEmpty() {
        if (!this.items.length) { UI.toast(I18N.t('trash.already_empty'), 'info'); return; }
        UI.confirmAction(I18N.t('trash.empty_confirm_title'), I18N.t('trash.empty_confirm_body'), I18N.t('trash.empty_button'), async () => {
            try {
                await API.trash.empty();
                UI.toast(I18N.t('trash.emptied'), 'success');
                this.load();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },
};
