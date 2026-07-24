/* Paylash — Chat (direct + group messaging) */
const ChatPage = {
    _conversations: [],
    _activeId: null,
    _activeConversation: null,
    _participants: [],
    _messages: [],
    _oldestLoadedId: null,
    _hasMoreHistory: true,
    _loadingMore: false,
    _pendingAttachments: [], // [{id, file_name, size_bytes}] already uploaded, not yet sent
    _pendingGroupMembers: [], // for the "new group" modal

    render() {
        return `
        <div class="chat-page">
            <div class="chat-sidebar">
                <div class="chat-sidebar-header">
                    <h3>${I18N.t('chat.title')}</h3>
                    <div class="chat-new-buttons">
                        <button class="btn btn-icon btn-ghost btn-sm" onclick="ChatPage.showNewDMModal()" title="${I18N.t('chat.new_dm')}" aria-label="${I18N.t('chat.new_dm')}">💬</button>
                        <button class="btn btn-icon btn-ghost btn-sm" onclick="ChatPage.showNewGroupModal()" title="${I18N.t('chat.new_group')}" aria-label="${I18N.t('chat.new_group')}">👥</button>
                    </div>
                </div>
                <div class="chat-conversation-list" id="chat-conversation-list">
                    <div class="empty-state"><div class="spinner"></div></div>
                </div>
            </div>
            <div class="chat-thread" id="chat-thread">
                <div class="empty-state"><p>${I18N.t('chat.select_conversation')}</p></div>
            </div>
        </div>`;
    },

    async init() {
        this._bindSocketListeners();
        await this.loadConversations();
        if (this._activeId) await this.selectConversation(this._activeId);
    },

    async loadConversations() {
        try { this._conversations = await API.chat.list() || []; }
        catch { this._conversations = []; }
        this.renderConversationList();
    },

    renderConversationList() {
        const el = document.getElementById('chat-conversation-list');
        if (!el) return;
        if (!this._conversations.length) {
            el.innerHTML = `<div class="empty-state"><p class="text-muted">${I18N.t('chat.no_conversations')}</p></div>`;
            return;
        }
        el.innerHTML = this._conversations.map(cv => this.conversationItemHTML(cv)).join('');
    },

    conversationItemHTML(cv) {
        const isDirect = cv.type === 'direct';
        const name = this.conversationName(cv);
        const avatar = isDirect && cv.other_participant
            ? UI.avatarHTML(cv.other_participant.user_id, cv.other_participant.full_name, 'chat-avatar')
            : `<span class="chat-avatar chat-avatar-group">👥</span>`;
        const preview = cv.last_message_body ? UI.esc(cv.last_message_body) : `<em>${I18N.t('chat.no_messages_yet')}</em>`;
        const badge = cv.unread_count > 0 ? `<span class="chat-unread-badge">${cv.unread_count > 99 ? '99+' : cv.unread_count}</span>` : '';
        return `<div class="chat-conv-item ${cv.id === this._activeId ? 'active' : ''}" onclick="ChatPage.selectConversation(${cv.id})" role="button" tabindex="0" onkeydown="if(event.key==='Enter')ChatPage.selectConversation(${cv.id})">
            ${avatar}
            <div class="chat-conv-item-body">
                <div class="chat-conv-item-name">${UI.esc(name)}</div>
                <div class="chat-conv-item-preview">${preview}</div>
            </div>
            ${badge}
        </div>`;
    },

    conversationName(cv) {
        if (cv.type === 'direct') {
            return cv.other_participant ? (cv.other_participant.full_name || cv.other_participant.username) : I18N.t('chat.unknown_user');
        }
        return cv.name || I18N.t('chat.unnamed_group');
    },

    async selectConversation(id) {
        this._activeId = id;
        this._messages = [];
        this._oldestLoadedId = null;
        this._hasMoreHistory = true;
        this.renderConversationList();

        const thread = document.getElementById('chat-thread');
        thread.innerHTML = `<div class="empty-state"><div class="spinner"></div></div>`;

        try {
            const detail = await API.chat.get(id);
            this._activeConversation = detail.conversation;
            this._participants = detail.participants || [];
        } catch (e) {
            thread.innerHTML = `<div class="empty-state"><p>${I18N.t('chat.load_failed')}</p></div>`;
            return;
        }

        try {
            this._messages = (await API.chat.listMessages(id)) || [];
            this._messages.reverse(); // API returns newest-first; render oldest-first
            if (this._messages.length) this._oldestLoadedId = this._messages[0].id;
            this._hasMoreHistory = this._messages.length >= 50;
        } catch { this._messages = []; }

        this.renderThread();
        try { await API.chat.markRead(id); } catch {}
        const cv = this._conversations.find(c => c.id === id);
        if (cv) { cv.unread_count = 0; this.renderConversationList(); }
        if (typeof App !== 'undefined') App.checkChatUnread();
    },

    renderThread() {
        const thread = document.getElementById('chat-thread');
        if (!thread || !this._activeConversation) return;
        const cv = this._activeConversation;
        const isDirect = cv.type === 'direct';
        const other = this._participants.find(p => p.user_id !== App.user.id);
        const name = isDirect ? (other ? (other.full_name || other.username) : I18N.t('chat.unknown_user')) : (cv.name || I18N.t('chat.unnamed_group'));

        thread.innerHTML = `
            <div class="chat-thread-header">
                <div class="chat-thread-title">${UI.esc(name)}</div>
                ${!isDirect ? `<button class="btn btn-ghost btn-sm" onclick="ChatPage.showGroupInfoModal()">${I18N.t('chat.group_info')}</button>` : ''}
            </div>
            <div class="chat-messages" id="chat-messages" onscroll="ChatPage.onMessagesScroll()">
                ${this._hasMoreHistory ? `<div class="chat-load-more" id="chat-load-more">${I18N.t('chat.load_older')}</div>` : ''}
                ${this._messages.map(m => this.messageHTML(m)).join('')}
            </div>
            <div class="chat-composer">
                <div id="chat-pending-attachments"></div>
                <div class="chat-composer-row">
                    <button class="btn btn-icon btn-ghost btn-sm" onclick="document.getElementById('chat-file-input').click()" title="${I18N.t('chat.attach_file')}" aria-label="${I18N.t('chat.attach_file')}">📎</button>
                    <input type="file" id="chat-file-input" multiple style="display:none" onchange="ChatPage.onFilesPicked(this.files)">
                    <textarea id="chat-message-input" class="form-control" rows="1" placeholder="${I18N.t('chat.message_placeholder')}" onkeydown="ChatPage.onComposerKeydown(event)"></textarea>
                    <button class="btn btn-primary btn-sm" onclick="ChatPage.sendMessage()">${I18N.t('chat.send')}</button>
                </div>
            </div>`;
        this.renderPendingAttachments();
        this.scrollToBottom();
    },

    messageHTML(m) {
        const mine = m.sender_id === App.user.id;
        const isDirect = this._activeConversation && this._activeConversation.type === 'direct';
        if (m.deleted_at) {
            return `<div class="chat-msg ${mine ? 'mine' : ''}"><div class="chat-msg-bubble chat-msg-deleted"><em>${I18N.t('chat.message_deleted')}</em></div></div>`;
        }
        const attachments = (m.attachments || []).map(a => this.attachmentHTML(a)).join('');
        const senderLabel = (!mine && !isDirect) ? `<div class="chat-msg-sender">${UI.esc(m.sender_name)}</div>` : '';
        const deleteBtn = mine ? `<button class="chat-msg-delete" onclick="ChatPage.deleteMessage(${m.id})" title="${I18N.t('common.delete')}" aria-label="${I18N.t('common.delete')}">✕</button>` : '';
        return `<div class="chat-msg ${mine ? 'mine' : ''}" id="chat-msg-${m.id}">
            <div class="chat-msg-bubble">
                ${senderLabel}
                ${m.body ? `<div class="chat-msg-body">${UI.esc(m.body)}</div>` : ''}
                ${attachments}
                <div class="chat-msg-time">${UI.formatDate(m.created_at)}</div>
            </div>
            ${deleteBtn}
        </div>`;
    },

    attachmentHTML(a) {
        const url = API.chat.attachmentDownloadURL(a.id);
        const icon = UI.fileIcon(a.file_name, false);
        return `<a class="chat-attachment" href="${url}" download="${UI.esc(a.file_name)}">
            <span class="chat-attachment-icon">${icon}</span>
            <span class="chat-attachment-name">${UI.esc(a.file_name)}</span>
            <span class="chat-attachment-size">${UI.formatBytes(a.size_bytes)}</span>
        </a>`;
    },

    scrollToBottom() {
        const el = document.getElementById('chat-messages');
        if (el) el.scrollTop = el.scrollHeight;
    },

    async onMessagesScroll() {
        const el = document.getElementById('chat-messages');
        if (!el || el.scrollTop > 40 || !this._hasMoreHistory || this._loadingMore) return;
        this._loadingMore = true;
        const prevHeight = el.scrollHeight;
        try {
            const older = (await API.chat.listMessages(this._activeId, this._oldestLoadedId)) || [];
            older.reverse();
            if (older.length) {
                this._oldestLoadedId = older[0].id;
                this._messages = [...older, ...this._messages];
            }
            this._hasMoreHistory = older.length >= 50;
            this.renderThread();
            const newEl = document.getElementById('chat-messages');
            if (newEl) newEl.scrollTop = newEl.scrollHeight - prevHeight;
        } catch { /* transient — user can just scroll again */ }
        this._loadingMore = false;
    },

    onComposerKeydown(ev) {
        if (ev.key === 'Enter' && !ev.shiftKey && !ev.isComposing) {
            ev.preventDefault();
            this.sendMessage();
        }
    },

    async onFilesPicked(fileList) {
        const files = Array.from(fileList);
        if (!files.length) return;
        const errors = await UI.runPooled(files, 3, async (file) => {
            const attachment = await API.chat.uploadAttachment(this._activeId, file);
            this._pendingAttachments.push(attachment);
        });
        if (errors.length) UI.toast(errors[0].error.message, 'error');
        document.getElementById('chat-file-input').value = '';
        this.renderPendingAttachments();
    },

    renderPendingAttachments() {
        const el = document.getElementById('chat-pending-attachments');
        if (!el) return;
        if (!this._pendingAttachments.length) { el.innerHTML = ''; return; }
        el.innerHTML = this._pendingAttachments.map((a, i) => `
            <span class="chat-pending-attachment">
                ${UI.fileIcon(a.file_name, false)} ${UI.esc(a.file_name)}
                <button class="chat-pending-remove" onclick="ChatPage.removePendingAttachment(${i})" aria-label="${I18N.t('common.remove')}">✕</button>
            </span>`).join('');
    },

    removePendingAttachment(index) {
        this._pendingAttachments.splice(index, 1);
        this.renderPendingAttachments();
    },

    async sendMessage() {
        const ta = document.getElementById('chat-message-input');
        const body = ta ? ta.value.trim() : '';
        if (!body && !this._pendingAttachments.length) return;
        const attachmentIds = this._pendingAttachments.map(a => a.id);
        try {
            const msg = await API.chat.send(this._activeId, body, attachmentIds);
            // The server broadcasts over the WS hub before this POST's own
            // response reaches the browser, so the live "message.new" echo
            // can genuinely arrive first (see the matching dedup check in
            // that handler below) — check here too, or whichever of the two
            // arrives second double-adds it.
            if (!this._messages.some(m => m.id === msg.id)) {
                this._messages.push(msg);
            }
            this._pendingAttachments = [];
            if (ta) ta.value = '';
            this.renderThread();
            this.loadConversations();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    deleteMessage(id) {
        UI.confirmAction(I18N.t('chat.delete_title'), I18N.t('chat.delete_body'), I18N.t('common.delete'), async () => {
            try {
                await API.chat.deleteMessage(this._activeId, id);
                const m = this._messages.find(x => x.id === id);
                if (m) { m.deleted_at = new Date().toISOString(); m.body = ''; m.attachments = []; }
                this.renderThread();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    /* ── New DM ── */

    showNewDMModal() {
        UI.showModal(I18N.t('chat.new_dm'), `
            <div class="form-group">
                <input type="text" id="chat-dm-search" class="form-control" placeholder="${I18N.t('chat.search_people_placeholder')}"
                    oninput="ChatPage.searchDMUsers(this.value)" onfocus="ChatPage.searchDMUsers(this.value)" autocomplete="off">
                <div id="chat-dm-results" class="share-user-results"></div>
            </div>`, '');
    },

    async searchDMUsers(q) {
        const r = document.getElementById('chat-dm-results');
        if (!r) return;
        try {
            const users = await API.chat.searchUsers(q || '');
            if (!users?.length) { r.innerHTML = `<div class="share-user-no-result">${I18N.t('shares.no_results')}</div>`; return; }
            r.innerHTML = users.map(u => `
                <div class="share-user-item" onclick="ChatPage.startDM(${u.id})">
                    ${UI.avatarHTML(u.id, u.full_name)}
                    <div><div class="share-user-name">${UI.esc(u.full_name)}</div><div class="share-user-username">@${UI.esc(u.username)}</div></div>
                </div>`).join('');
        } catch { r.innerHTML = ''; }
    },

    async startDM(userId) {
        try {
            const conv = await API.chat.createDirect(userId);
            UI.closeModal();
            await this.loadConversations();
            await this.selectConversation(conv.id);
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── New group ── */

    showNewGroupModal() {
        this._pendingGroupMembers = [];
        UI.showModal(I18N.t('chat.new_group'), `
            <div class="form-group">
                <label>${I18N.t('chat.group_name_label')}</label>
                <input type="text" id="chat-group-name" class="form-control" placeholder="${I18N.t('chat.group_name_placeholder')}">
            </div>
            <div class="form-group">
                <label>${I18N.t('chat.group_members_label')}</label>
                <input type="text" id="chat-group-search" class="form-control" placeholder="${I18N.t('chat.search_people_placeholder')}"
                    oninput="ChatPage.searchGroupUsers(this.value)" onfocus="ChatPage.searchGroupUsers(this.value)" autocomplete="off">
                <div id="chat-group-results" class="share-user-results"></div>
            </div>
            <div id="chat-group-pending"></div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="ChatPage.createGroup()">${I18N.t('common.create')}</button>`);
    },

    async searchGroupUsers(q) {
        const r = document.getElementById('chat-group-results');
        if (!r) return;
        try {
            const users = await API.chat.searchUsers(q || '');
            const skipIds = new Set(this._pendingGroupMembers.map(m => m.id));
            const filtered = (users || []).filter(u => !skipIds.has(u.id));
            if (!filtered.length) { r.innerHTML = `<div class="share-user-no-result">${I18N.t('shares.no_results')}</div>`; return; }
            r.innerHTML = filtered.map(u => `
                <div class="share-user-item" onclick="ChatPage.addGroupMember(${u.id},${UI.escJson(u.full_name)},${UI.escJson(u.username)})">
                    ${UI.avatarHTML(u.id, u.full_name)}
                    <div><div class="share-user-name">${UI.esc(u.full_name)}</div><div class="share-user-username">@${UI.esc(u.username)}</div></div>
                </div>`).join('');
        } catch { r.innerHTML = ''; }
    },

    addGroupMember(id, name, username) {
        if (this._pendingGroupMembers.some(m => m.id === id)) return;
        this._pendingGroupMembers.push({ id, name, username });
        document.getElementById('chat-group-search').value = '';
        document.getElementById('chat-group-results').innerHTML = '';
        this.renderPendingGroupMembers();
    },

    removeGroupMember(id) {
        this._pendingGroupMembers = this._pendingGroupMembers.filter(m => m.id !== id);
        this.renderPendingGroupMembers();
    },

    renderPendingGroupMembers() {
        const el = document.getElementById('chat-group-pending');
        if (!el) return;
        el.innerHTML = this._pendingGroupMembers.map(m => `
            <span class="chat-pending-attachment">
                ${UI.esc(m.name)}
                <button class="chat-pending-remove" onclick="ChatPage.removeGroupMember(${m.id})" aria-label="${I18N.t('common.remove')}">✕</button>
            </span>`).join('');
    },

    async createGroup() {
        const name = document.getElementById('chat-group-name')?.value.trim();
        if (!name) { UI.toast(I18N.t('chat.group_name_required'), 'error'); return; }
        if (!this._pendingGroupMembers.length) { UI.toast(I18N.t('chat.group_members_required'), 'error'); return; }
        try {
            const conv = await API.chat.createGroup(name, this._pendingGroupMembers.map(m => m.id));
            UI.closeModal();
            await this.loadConversations();
            await this.selectConversation(conv.id);
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    /* ── Group info / manage ── */

    showGroupInfoModal() {
        const cv = this._activeConversation;
        if (!cv) return;
        const isCreator = cv.created_by === App.user.id;
        UI.showModal(I18N.t('chat.group_info'), `
            ${isCreator ? `
            <div class="form-group">
                <label>${I18N.t('chat.group_name_label')}</label>
                <div style="display:flex;gap:8px">
                    <input type="text" id="chat-rename-input" class="form-control" value="${UI.esc(cv.name || '')}">
                    <button class="btn btn-ghost btn-sm" onclick="ChatPage.renameGroup()">${I18N.t('common.save')}</button>
                </div>
            </div>` : ''}
            <div class="form-group">
                <label>${I18N.t('chat.members_label')}</label>
                <div id="chat-members-list">${this._participants.map(p => this.memberRowHTML(p, isCreator)).join('')}</div>
            </div>
            ${isCreator ? `
            <div class="form-group">
                <input type="text" id="chat-add-member-search" class="form-control" placeholder="${I18N.t('chat.search_people_placeholder')}"
                    oninput="ChatPage.searchAddMember(this.value)" autocomplete="off">
                <div id="chat-add-member-results" class="share-user-results"></div>
            </div>` : ''}`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.close')}</button>
             <button class="btn btn-danger" onclick="ChatPage.leaveGroup()">${I18N.t('chat.leave_group')}</button>`);
    },

    memberRowHTML(p, isCreator) {
        const canRemove = isCreator && p.user_id !== App.user.id;
        return `<div class="chat-member-row">
            ${UI.avatarHTML(p.user_id, p.full_name, 'chat-avatar-sm')}
            <span>${UI.esc(p.full_name || p.username)}</span>
            ${canRemove ? `<button class="chat-pending-remove" onclick="ChatPage.removeMember(${p.user_id})" aria-label="${I18N.t('common.remove')}">✕</button>` : ''}
        </div>`;
    },

    async renameGroup() {
        const name = document.getElementById('chat-rename-input')?.value.trim();
        if (!name) return;
        try {
            await API.chat.rename(this._activeId, name);
            this._activeConversation.name = name;
            UI.closeModal();
            this.renderThread();
            this.loadConversations();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async searchAddMember(q) {
        const r = document.getElementById('chat-add-member-results');
        if (!r || !q || q.length < 2) { if (r) r.innerHTML = ''; return; }
        try {
            const users = await API.chat.searchUsers(q);
            const existingIds = new Set(this._participants.map(p => p.user_id));
            const filtered = (users || []).filter(u => !existingIds.has(u.id));
            r.innerHTML = filtered.map(u => `
                <div class="share-user-item" onclick="ChatPage.addMember(${u.id})">
                    ${UI.avatarHTML(u.id, u.full_name)}
                    <div><div class="share-user-name">${UI.esc(u.full_name)}</div></div>
                </div>`).join('') || `<div class="share-user-no-result">${I18N.t('shares.no_results')}</div>`;
        } catch { r.innerHTML = ''; }
    },

    async addMember(userId) {
        try {
            await API.chat.addParticipants(this._activeId, [userId]);
            const detail = await API.chat.get(this._activeId);
            this._participants = detail.participants || [];
            this.showGroupInfoModal();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    async removeMember(userId) {
        try {
            await API.chat.removeParticipant(this._activeId, userId);
            this._participants = this._participants.filter(p => p.user_id !== userId);
            this.showGroupInfoModal();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    leaveGroup() {
        UI.confirmAction(I18N.t('chat.leave_group'), I18N.t('chat.leave_group_confirm'), I18N.t('chat.leave_group'), async () => {
            try {
                await API.chat.removeParticipant(this._activeId, App.user.id);
                this._activeId = null;
                this._activeConversation = null;
                document.getElementById('chat-thread').innerHTML = `<div class="empty-state"><p>${I18N.t('chat.select_conversation')}</p></div>`;
                await this.loadConversations();
                if (typeof App !== 'undefined') App.checkChatUnread();
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    /* ── Live updates via ChatSocket ── */

    _socketBound: false,
    _bindSocketListeners() {
        if (this._socketBound) return;
        this._socketBound = true;
        ChatSocket.on('message.new', (data) => {
            if (App.currentPage !== 'chat') return;
            if (data.conversation_id === this._activeId) {
                // The sender's own tab already appended this optimistically
                // in sendMessage() before the WS echo arrives — broadcast
                // deliberately includes the sender too (so their OTHER open
                // tabs/devices also get it), so dedup by id rather than by
                // sender_id here, or a second tab of the same user would
                // silently never receive the message at all.
                if (!this._messages.some(m => m.id === data.message.id)) {
                    this._messages.push(data.message);
                    this.renderThread();
                }
                API.chat.markRead(this._activeId).catch(() => {});
            }
            this.loadConversations();
        });
        ChatSocket.on('message.deleted', (data) => {
            if (App.currentPage !== 'chat') return;
            if (data.conversation_id === this._activeId) {
                const m = this._messages.find(x => x.id === data.message_id);
                if (m) { m.deleted_at = new Date().toISOString(); m.body = ''; m.attachments = []; }
                this.renderThread();
            }
        });
        ChatSocket.on('conversation.updated', (data) => {
            if (App.currentPage !== 'chat') return;
            this.loadConversations();
            if (data.conversation_id === this._activeId) this.selectConversation(this._activeId);
        });
    },
};
