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
    // Composer context is either a reply-in-progress or an edit-in-progress —
    // never both at once (starting one clears the other), same as Telegram.
    _composerContext: null, // { type: 'reply', id, senderName, body, kind } | { type: 'edit', id }
    _pendingSeq: 0,
    _forwardMessageId: null,
    _forwardSelected: null,
    // Id of the first unread message when the conversation was opened, so a
    // "new messages" divider can be drawn above it (cleared on next open once
    // markRead advances the server-side last_read_at).
    _unreadFromId: null,
    // Count of messages that arrived while the user was scrolled up, shown on
    // the floating "jump to newest" pill so they know something came in below.
    _newBelow: 0,
    // Live typing state: _typing[conversationId] = { [userId]: {name, timer} }.
    // Each entry auto-expires ~5s after the last "typing" ping for it.
    _typing: {},
    _lastTypingSent: 0,

    STICKERS: {
        faces: ['😀', '😂', '🥰', '😎', '😢', '😡', '😮', '🥳', '😴', '🤔'],
        gestures: ['👍', '👎', '👏', '🙌', '🤝', '🙏', '💪', '✌️', '👌', '🤞'],
        hearts: ['❤️', '💙', '💚', '💛', '💜', '🧡', '💔', '💯', '✨', '🔥'],
        celebration: ['🎉', '🎊', '🎁', '🏆', '🥂', '🎈', '🌟', '⭐', '👏', '🍾'],
        studio: ['🏗️', '🏛️', '📐', '📏', '🧱', '🏠', '🖼️', '📁', '✏️', '🗺️'],
    },

    // Quick reactions — kept in sync with reactionSet in internal/api/reactions.go
    // (the server rejects anything not on its own copy of this list).
    REACTIONS: ['👍', '❤️', '😂', '😮', '😢', '🙏', '🔥', '🎉'],

    render() {
        return `
        <div class="chat-page ${this._activeId ? 'chat-mobile-open' : ''}">
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

    _updateMobileClass() {
        const page = document.querySelector('.chat-page');
        if (page) page.classList.toggle('chat-mobile-open', !!this._activeId);
    },

    backToList() {
        this._activeId = null;
        this._activeConversation = null;
        this._updateMobileClass();
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
        const innerAvatar = isDirect && cv.other_participant
            ? UI.avatarHTML(cv.other_participant.user_id, cv.other_participant.full_name, 'chat-avatar')
            : `<span class="chat-avatar chat-avatar-group">👥</span>`;
        const online = isDirect && cv.other_participant && cv.other_participant.online;
        const avatar = `<span class="chat-avatar-wrap">${innerAvatar}${online ? '<span class="chat-presence-dot"></span>' : ''}</span>`;
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
        this._composerContext = null;
        this._updateMobileClass();
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

        // Unread boundary: the first message from someone else that arrived
        // after this viewer last read the conversation — computed from the
        // participant list's last_read_at BEFORE markRead below advances it.
        this._unreadFromId = null;
        this._newBelow = 0;
        const me = this._participants.find(p => p.user_id === App.user.id);
        const lastReadMs = me ? new Date(me.last_read_at).getTime() : 0;
        for (const m of this._messages) {
            if (m.sender_id !== App.user.id && new Date(m.created_at).getTime() > lastReadMs) {
                this._unreadFromId = m.id;
                break;
            }
        }

        this.renderThread();
        try { await API.chat.markRead(id); } catch {}
        const cv = this._conversations.find(c => c.id === id);
        if (cv) { cv.unread_count = 0; this.renderConversationList(); }
        if (typeof App !== 'undefined') App.checkChatUnread();
    },

    // Builds the thread's stable shell (header + empty messages container +
    // composer) exactly once per conversation open. Live events afterwards
    // mutate individual message nodes (append/replace/remove) rather than
    // rebuilding this whole subtree — that preserves scroll position, keeps
    // focus, and, crucially, never wipes text the user is mid-way through
    // typing when an incoming message arrives.
    renderThread() {
        const thread = document.getElementById('chat-thread');
        if (!thread || !this._activeConversation) return;
        const cv = this._activeConversation;
        const isDirect = cv.type === 'direct';
        const other = this._participants.find(p => p.user_id !== App.user.id);
        const name = isDirect ? (other ? (other.full_name || other.username) : I18N.t('chat.unknown_user')) : (cv.name || I18N.t('chat.unnamed_group'));
        const headerInner = isDirect && other
            ? UI.avatarHTML(other.user_id, other.full_name || other.username, 'chat-avatar-sm chat-thread-avatar')
            : `<span class="chat-avatar-sm chat-avatar-group chat-thread-avatar">👥</span>`;
        const headerAvatar = `<span class="chat-avatar-wrap">${headerInner}${isDirect && other && other.online ? '<span class="chat-presence-dot"></span>' : ''}</span>`;

        thread.innerHTML = `
            <div class="chat-thread-header">
                <button class="chat-back-btn" onclick="ChatPage.backToList()" aria-label="${I18N.t('chat.back_to_list')}">←</button>
                ${headerAvatar}
                <div class="chat-thread-heading">
                    <div class="chat-thread-title">${UI.esc(name)}</div>
                    <div class="chat-thread-subtitle" id="chat-thread-subtitle"></div>
                </div>
                ${!isDirect ? `<button class="btn btn-ghost btn-sm" onclick="ChatPage.showGroupInfoModal()">${I18N.t('chat.group_info')}</button>` : ''}
            </div>
            <div class="chat-messages" id="chat-messages" onscroll="ChatPage.onMessagesScroll()"></div>
            <button class="chat-scroll-down hidden" id="chat-scroll-down" onclick="ChatPage.jumpToBottom()" aria-label="${I18N.t('chat.scroll_to_latest')}">
                <span class="chat-scroll-down-arrow">↓</span>
                <span class="chat-scroll-down-count" id="chat-scroll-down-count"></span>
            </button>
            <div class="chat-composer">
                <div id="chat-reply-bar"></div>
                <div id="chat-pending-attachments"></div>
                <div class="chat-composer-row">
                    <button class="btn btn-icon btn-ghost btn-sm" onclick="document.getElementById('chat-file-input').click()" title="${I18N.t('chat.attach_file')}" aria-label="${I18N.t('chat.attach_file')}">📎</button>
                    <button class="btn btn-icon btn-ghost btn-sm chat-sticker-toggle-btn" onclick="ChatPage.toggleStickerPicker()" title="${I18N.t('chat.stickers')}" aria-label="${I18N.t('chat.stickers')}">😊</button>
                    <input type="file" id="chat-file-input" multiple style="display:none" onchange="ChatPage.onFilesPicked(this.files)">
                    <textarea id="chat-message-input" class="form-control" rows="1" placeholder="${I18N.t('chat.message_placeholder')}" onkeydown="ChatPage.onComposerKeydown(event)" oninput="ChatPage.onComposerInput()"></textarea>
                    <button class="btn btn-primary btn-sm" onclick="ChatPage.sendMessage()">${I18N.t('chat.send')}</button>
                </div>
            </div>`;
        this.renderMessages();
        this.renderPendingAttachments();
        this.renderReplyBar();
        this.renderSubtitle();
        // Open on the first unread message when there is one, otherwise at the
        // very bottom — same instinct as every messenger.
        if (this._unreadFromId) {
            const divider = document.getElementById('chat-unread-divider');
            if (divider) divider.scrollIntoView({ block: 'center' });
            else this.scrollToBottom();
        } else {
            this.scrollToBottom();
        }
        this.updateScrollDownPill();
    },

    // Rebuilds only the messages container (never the composer), from the
    // in-memory _messages list — used for the initial render, history
    // prepends, and any structural change where recomputing day-dividers and
    // grouping from scratch is simpler than patching around a removed node.
    renderMessages() {
        const el = document.getElementById('chat-messages');
        if (!el) return;
        const parts = [];
        if (this._hasMoreHistory) {
            parts.push(`<div class="chat-load-more" id="chat-load-more" onclick="ChatPage.loadOlder()">${I18N.t('chat.load_older')}</div>`);
        }
        for (const item of this.groupMessagesForRender(this._messages)) {
            if (item.type === 'day') parts.push(this.dayDividerHTML(item.label));
            else if (item.type === 'unread') parts.push(this.unreadDividerHTML());
            else parts.push(this.messageHTML(item.message, item.isGroupStart));
        }
        el.innerHTML = parts.join('') || `<div class="empty-state"><p class="text-muted">${I18N.t('chat.no_messages_yet')}</p></div>`;
    },

    dayDividerHTML(label) {
        return `<div class="chat-day-divider"><span>${UI.esc(label)}</span></div>`;
    },

    unreadDividerHTML() {
        return `<div class="chat-unread-divider" id="chat-unread-divider"><span>${I18N.t('chat.unread_messages')}</span></div>`;
    },

    // Splits a flat, oldest-first message list into day-divider chips and
    // grouped-message runs (same sender within 5 minutes, no reply/forward
    // in between) — a message starting a new group gets the avatar/sender
    // name/extra top margin; the rest of its run sits tight underneath.
    groupMessagesForRender(messages) {
        const items = [];
        let lastDayKey = null;
        let lastSenderKey = null;
        let lastTime = 0;
        for (const m of messages) {
            const d = new Date(m.created_at);
            const dayKey = d.toDateString();
            if (dayKey !== lastDayKey) {
                items.push({ type: 'day', label: this.dayLabel(d) });
                lastDayKey = dayKey;
                lastSenderKey = null;
            }
            if (m.id === this._unreadFromId) {
                items.push({ type: 'unread' });
                lastSenderKey = null; // force the first unread to start a fresh group under the divider
            }
            const isGroupStart = m.sender_id !== lastSenderKey || (d.getTime() - lastTime) > 5 * 60 * 1000 || !!m.reply_to || !!m.forwarded_from_name;
            items.push({ type: 'msg', message: m, isGroupStart });
            lastSenderKey = m.sender_id;
            lastTime = d.getTime();
        }
        return items;
    },

    // Recomputes whether `m` starts a new visual group relative to the message
    // immediately before it — the incremental-append counterpart to the
    // grouping logic in groupMessagesForRender.
    computeGroupStart(m, prev) {
        if (!prev) return true;
        const d = new Date(m.created_at), pd = new Date(prev.created_at);
        if (d.toDateString() !== pd.toDateString()) return true;
        return m.sender_id !== prev.sender_id || (d.getTime() - pd.getTime()) > 5 * 60 * 1000 || !!m.reply_to || !!m.forwarded_from_name;
    },

    dayLabel(d) {
        const today = new Date();
        const yest = new Date();
        yest.setDate(today.getDate() - 1);
        if (d.toDateString() === today.toDateString()) return I18N.t('chat.day_today');
        if (d.toDateString() === yest.toDateString()) return I18N.t('chat.day_yesterday');
        return d.toLocaleDateString();
    },

    messageHTML(m, isGroupStart) {
        const mine = m.sender_id === App.user.id;
        const isDirect = this._activeConversation && this._activeConversation.type === 'direct';
        const groupCls = isGroupStart ? ' chat-msg-group-start' : '';
        if (m.deleted_at) {
            return `<div class="chat-msg ${mine ? 'mine' : ''}${groupCls}" id="chat-msg-${m.id}">
                <div class="chat-msg-bubble chat-msg-deleted"><em>${I18N.t('chat.message_deleted')}</em></div>
            </div>`;
        }
        const avatarSlot = (!mine && !isDirect)
            ? (isGroupStart ? UI.avatarHTML(m.sender_id, m.sender_name, 'chat-avatar-sm chat-msg-avatar') : '<span class="chat-msg-avatar-spacer"></span>')
            : '';
        if (m.kind === 'sticker') {
            return `<div class="chat-msg chat-msg-sticker-row ${mine ? 'mine' : ''}${groupCls}" id="chat-msg-${m.id}" oncontextmenu="ChatPage.showMessageMenu(event,${m.id})">
                ${avatarSlot}
                <div class="chat-sticker-wrap">
                    <div class="chat-sticker">${UI.esc(m.body)}</div>
                    <div class="chat-msg-time chat-sticker-time">${UI.formatDate(m.created_at)} ${this.statusTickHTML(m, mine)}</div>
                    ${this.reactionsBarHTML(m)}
                </div>
                <button class="chat-msg-menu-btn" onclick="ChatPage.showMessageMenu(event,${m.id})" title="${I18N.t('common.actions')}" aria-label="${I18N.t('common.actions')}">⋮</button>
            </div>`;
        }
        const attachments = (m.attachments || []).map(a => this.attachmentHTML(a)).join('');
        const senderLabel = (!mine && !isDirect && isGroupStart) ? `<div class="chat-msg-sender">${UI.esc(m.sender_name)}</div>` : '';
        const replyBlock = m.reply_to ? `<div class="chat-reply-preview" onclick="ChatPage.jumpToMessage(${m.reply_to.id})">
            <span class="chat-reply-sender">${UI.esc(m.reply_to.sender_name)}</span>
            <span class="chat-reply-text">${UI.esc(m.reply_to.kind === 'sticker' ? m.reply_to.body : (m.reply_to.body || I18N.t('chat.attachment_notification')))}</span>
        </div>` : '';
        const forwardedBlock = m.forwarded_from_name ? `<div class="chat-forwarded-label">↪ ${I18N.t('chat.forwarded_from')} ${UI.esc(m.forwarded_from_name)}</div>` : '';
        const editedLabel = m.edited_at ? `<span class="chat-edited-label">${I18N.t('chat.edited_label')}</span>` : '';
        return `<div class="chat-msg ${mine ? 'mine' : ''}${groupCls}" id="chat-msg-${m.id}">
            ${avatarSlot}
            <div class="chat-msg-bubble" oncontextmenu="ChatPage.showMessageMenu(event,${m.id})">
                ${forwardedBlock}
                ${senderLabel}
                ${replyBlock}
                ${this.messageBodyHTML(m)}
                ${attachments}
                <div class="chat-msg-time">${editedLabel} ${UI.formatDate(m.created_at)} ${this.statusTickHTML(m, mine)}</div>
                ${this.reactionsBarHTML(m)}
            </div>
            <button class="chat-msg-menu-btn" onclick="ChatPage.showMessageMenu(event,${m.id})" title="${I18N.t('common.actions')}" aria-label="${I18N.t('common.actions')}">⋮</button>
        </div>`;
    },

    // Message body with URLs turned into safe links. UI.escLinkify escapes
    // first (so no markup survives) and only then wraps http(s) URLs — the
    // one place a message's own text becomes interactive, done XSS-safely.
    messageBodyHTML(m) {
        if (!m.body) return '';
        return `<div class="chat-msg-body">${UI.escLinkify(m.body)}</div>`;
    },

    // The reaction chips shown under a message. Each chip is one emoji plus
    // how many people used it, highlighted when the viewer is one of them;
    // clicking toggles the viewer's own reaction.
    reactionsBarHTML(m) {
        if (!m.reactions || !m.reactions.length) return '';
        const chips = m.reactions.map(g => {
            const ids = g.user_ids || [];
            const mine = ids.includes(App.user.id);
            return `<button class="chat-reaction ${mine ? 'mine' : ''}" onclick="ChatPage.toggleReaction(${m.id},${UI.escJson(g.emoji)})">
                <span class="chat-reaction-emoji">${UI.esc(g.emoji)}</span><span class="chat-reaction-count">${ids.length}</span>
            </button>`;
        }).join('');
        return `<div class="chat-reactions">${chips}</div>`;
    },

    // Sent/read ticks — only meaningful for the viewer's own messages.
    // "read" only ever comes from the server for direct conversations (see
    // MessageView.Status server-side); group messages always land as "sent".
    statusTickHTML(m, mine) {
        if (!mine) return '';
        if (m._pending) return `<span class="chat-msg-status pending" title="${I18N.t('chat.status_pending')}">🕓</span>`;
        if (m.status === 'read') return `<span class="chat-msg-status read" title="${I18N.t('chat.status_read')}">✓✓</span>`;
        return `<span class="chat-msg-status sent" title="${I18N.t('chat.status_sent')}">✓</span>`;
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

    _isNearBottom(px) {
        const el = document.getElementById('chat-messages');
        if (!el) return true;
        return el.scrollHeight - el.scrollTop - el.clientHeight < (px || 120);
    },

    jumpToBottom() {
        this._newBelow = 0;
        this.scrollToBottom();
        this.updateScrollDownPill();
    },

    // Floating "jump to newest" pill: visible whenever the user is scrolled
    // up, badged with how many messages have landed below since they left the
    // bottom (so a message arriving mid-scrollback is noticed, not missed).
    updateScrollDownPill() {
        const btn = document.getElementById('chat-scroll-down');
        if (!btn) return;
        const show = !this._isNearBottom(120);
        btn.classList.toggle('hidden', !show);
        if (!show) this._newBelow = 0;
        const countEl = document.getElementById('chat-scroll-down-count');
        if (countEl) {
            countEl.textContent = this._newBelow > 0 ? (this._newBelow > 99 ? '99+' : String(this._newBelow)) : '';
            countEl.classList.toggle('hidden', this._newBelow <= 0);
        }
    },

    jumpToMessage(id) {
        const el = document.getElementById('chat-msg-' + id);
        if (!el) { UI.toast(I18N.t('chat.reply_not_loaded'), 'info'); return; }
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        el.classList.add('chat-msg-flash');
        setTimeout(() => el.classList.remove('chat-msg-flash'), 1200);
    },

    onMessagesScroll() {
        this.updateScrollDownPill();
        const el = document.getElementById('chat-messages');
        if (el && el.scrollTop <= 40) this.loadOlder();
    },

    async loadOlder() {
        const el = document.getElementById('chat-messages');
        if (!el || !this._hasMoreHistory || this._loadingMore) return;
        this._loadingMore = true;
        const prevHeight = el.scrollHeight;
        const prevTop = el.scrollTop;
        try {
            const older = (await API.chat.listMessages(this._activeId, this._oldestLoadedId)) || [];
            older.reverse();
            if (older.length) {
                this._oldestLoadedId = older[0].id;
                this._messages = [...older, ...this._messages];
            }
            this._hasMoreHistory = older.length >= 50;
            // renderMessages (not renderThread) so the composer — and anything
            // typed into it — is left completely untouched.
            this.renderMessages();
            const newEl = document.getElementById('chat-messages');
            if (newEl) newEl.scrollTop = newEl.scrollHeight - prevHeight + prevTop;
        } catch { /* transient — user can just scroll again */ }
        this._loadingMore = false;
    },

    autoGrowComposer() {
        const ta = document.getElementById('chat-message-input');
        if (!ta) return;
        ta.style.height = 'auto';
        ta.style.height = Math.min(ta.scrollHeight, 140) + 'px';
    },

    onComposerInput() {
        this.autoGrowComposer();
        this.signalTyping();
    },

    // Fires a "typing" ping up the socket, throttled to at most once every 3s
    // (the server also throttles, and receivers auto-expire the indicator).
    signalTyping() {
        if (!this._activeId) return;
        const now = Date.now();
        if (now - this._lastTypingSent < 3000) return;
        this._lastTypingSent = now;
        ChatSocket.send({ type: 'typing', conversation_id: this._activeId });
    },

    /* ── Typing indicator (incoming) ── */

    handleTyping(data) {
        const convID = data.conversation_id;
        if (!convID || data.user_id === App.user.id) return;
        const map = (this._typing[convID] = this._typing[convID] || {});
        if (map[data.user_id] && map[data.user_id].timer) clearTimeout(map[data.user_id].timer);
        map[data.user_id] = {
            name: data.user_name || I18N.t('chat.someone'),
            timer: setTimeout(() => this.clearTyping(convID, data.user_id), 5000),
        };
        if (convID === this._activeId) this.renderSubtitle();
    },

    clearTyping(convID, userID) {
        const map = this._typing[convID];
        if (!map || !map[userID]) return;
        if (map[userID].timer) clearTimeout(map[userID].timer);
        delete map[userID];
        if (convID === this._activeId) this.renderSubtitle();
    },

    typingNames(convID) {
        const map = this._typing[convID];
        if (!map) return [];
        return Object.values(map).map(v => v.name);
    },

    /* ── Presence (incoming) ── */

    handlePresence(data) {
        // Patch the live participant list of the open conversation…
        const p = this._participants.find(x => x.user_id === data.user_id);
        if (p) {
            p.online = data.online;
            if (data.last_seen_at) p.last_seen_at = data.last_seen_at;
        }
        // …and the DM online dots in the inbox list.
        let listChanged = false;
        this._conversations.forEach(cv => {
            if (cv.other_participant && cv.other_participant.user_id === data.user_id) {
                cv.other_participant.online = data.online;
                if (data.last_seen_at) cv.other_participant.last_seen_at = data.last_seen_at;
                listChanged = true;
            }
        });
        if (listChanged) this.renderConversationList();
        if (p && this._activeConversation && this._activeConversation.type === 'direct') this.renderSubtitle();
    },

    /* ── Thread-header subtitle: typing / presence / member count ── */

    renderSubtitle() {
        const el = document.getElementById('chat-thread-subtitle');
        if (!el || !this._activeConversation) return;
        const cv = this._activeConversation;
        el.className = 'chat-thread-subtitle';

        const typers = this.typingNames(this._activeId);
        if (typers.length) {
            el.classList.add('typing');
            el.textContent = typers.length === 1
                ? I18N.t('chat.typing_one', { name: typers[0] })
                : I18N.t('chat.typing_many');
            return;
        }

        if (cv.type === 'direct') {
            const other = this._participants.find(x => x.user_id !== App.user.id);
            if (!other) { el.textContent = ''; return; }
            if (other.online) {
                el.classList.add('online');
                el.textContent = I18N.t('chat.online');
            } else {
                el.textContent = other.last_seen_at
                    ? I18N.t('chat.last_seen', { time: this.lastSeenText(other.last_seen_at) })
                    : I18N.t('chat.offline');
            }
            return;
        }

        // Group: member count, plus how many are online right now.
        const total = this._participants.length;
        const online = this._participants.filter(x => x.online).length;
        el.textContent = online > 1
            ? I18N.tn('chat.members_count', total, { count: total }) + ' · ' + I18N.t('chat.n_online', { count: online })
            : I18N.tn('chat.members_count', total, { count: total });
    },

    // Human "last seen" text: just now / N minutes / today HH:MM / a date.
    lastSeenText(iso) {
        const then = new Date(iso), now = new Date();
        const diffMin = Math.floor((now - then) / 60000);
        if (diffMin < 1) return I18N.t('chat.seen_just_now');
        if (diffMin < 60) return I18N.tn('chat.seen_minutes', diffMin, { count: diffMin });
        if (then.toDateString() === now.toDateString()) {
            return I18N.t('chat.seen_today', { time: then.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) });
        }
        return then.toLocaleDateString();
    },

    /* ── Incremental message-node updates (no full re-render) ── */

    // Appends one just-arrived message, keeping _messages and the DOM in sync.
    // Idempotent by id: a message already shown (e.g. the WS echo of one this
    // tab just sent) is ignored rather than duplicated.
    appendMessage(m) {
        if (this._messages.some(x => x.id === m.id)) return;
        const el = document.getElementById('chat-messages');
        if (!el) { this._messages.push(m); return; }
        const empty = el.querySelector('.empty-state');
        if (empty) el.innerHTML = '';
        const prev = this._messages.length ? this._messages[this._messages.length - 1] : null;
        const mine = m.sender_id === App.user.id;
        const wasNearBottom = this._isNearBottom(120);
        let html = '';
        if (!prev || new Date(m.created_at).toDateString() !== new Date(prev.created_at).toDateString()) {
            html += this.dayDividerHTML(this.dayLabel(new Date(m.created_at)));
        }
        html += this.messageHTML(m, this.computeGroupStart(m, prev));
        this._messages.push(m);
        el.insertAdjacentHTML('beforeend', html);
        if (mine || wasNearBottom) {
            this.scrollToBottom();
        } else {
            this._newBelow++;
        }
        this.updateScrollDownPill();
    },

    // Replaces one message's node in place (edit, reaction, status, deleted
    // tombstone). The caller has already updated the _messages entry; the
    // node keeps whatever group-start it had since its neighbors are unchanged.
    replaceMessage(m) {
        const node = document.getElementById('chat-msg-' + m.id);
        if (!node) return;
        node.outerHTML = this.messageHTML(m, node.classList.contains('chat-msg-group-start'));
    },

    // Removes one message (delete-for-me) then re-renders the messages list so
    // day-dividers/grouping stay correct — composer is left untouched.
    removeMessage(id) {
        const idx = this._messages.findIndex(x => x.id === id);
        if (idx !== -1) this._messages.splice(idx, 1);
        this.renderMessages();
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

    /* ── Reply / edit composer context ── */

    replyToMessage(id) {
        const m = this._messages.find(x => x.id === id);
        if (!m || m.deleted_at) return;
        this._composerContext = {
            type: 'reply', id: m.id,
            senderName: m.sender_id === App.user.id ? I18N.t('chat.you') : m.sender_name,
            body: m.body, kind: m.kind,
        };
        this.renderReplyBar();
        document.getElementById('chat-message-input')?.focus();
    },

    startEditMessage(id) {
        const m = this._messages.find(x => x.id === id);
        if (!m || m.kind !== 'text' || m.deleted_at || m.sender_id !== App.user.id) return;
        this._composerContext = { type: 'edit', id };
        this.renderReplyBar();
        const ta = document.getElementById('chat-message-input');
        if (ta) { ta.value = m.body; ta.focus(); }
    },

    cancelComposerContext() {
        const wasEdit = this._composerContext && this._composerContext.type === 'edit';
        this._composerContext = null;
        this.renderReplyBar();
        if (wasEdit) {
            const ta = document.getElementById('chat-message-input');
            if (ta) ta.value = '';
        }
    },

    renderReplyBar() {
        const el = document.getElementById('chat-reply-bar');
        if (!el) return;
        const ctx = this._composerContext;
        if (!ctx) { el.innerHTML = ''; return; }
        if (ctx.type === 'edit') {
            el.innerHTML = `<div class="chat-composer-context chat-composer-editing">
                <div class="chat-composer-context-text"><strong>${I18N.t('chat.editing_message')}</strong></div>
                <button class="chat-pending-remove" onclick="ChatPage.cancelComposerContext()" aria-label="${I18N.t('common.cancel')}">✕</button>
            </div>`;
            return;
        }
        el.innerHTML = `<div class="chat-composer-context">
            <div class="chat-composer-context-text"><strong>${UI.esc(ctx.senderName)}</strong><span>${UI.esc(ctx.kind === 'sticker' ? ctx.body : (ctx.body || I18N.t('chat.attachment_notification')))}</span></div>
            <button class="chat-pending-remove" onclick="ChatPage.cancelComposerContext()" aria-label="${I18N.t('common.cancel')}">✕</button>
        </div>`;
    },

    /* ── Send / edit / delete ── */

    async sendMessage() {
        const ta = document.getElementById('chat-message-input');
        const body = ta ? ta.value.trim() : '';
        const ctx = this._composerContext;

        if (ctx && ctx.type === 'edit') {
            if (!body) return;
            try {
                const updated = await API.chat.editMessage(this._activeId, ctx.id, body);
                const idx = this._messages.findIndex(m => m.id === ctx.id);
                if (idx !== -1) this._messages[idx] = updated;
                this._composerContext = null;
                if (ta) { ta.value = ''; }
                this.renderReplyBar();
                this.autoGrowComposer();
                this.replaceMessage(updated);
            } catch (e) { UI.toast(e.message, 'error'); }
            return;
        }

        if (!body && !this._pendingAttachments.length) return;
        const attachmentIds = this._pendingAttachments.map(a => a.id);
        const replyToId = ctx && ctx.type === 'reply' ? ctx.id : null;

        // Optimistic "sending" placeholder — swapped for the real message (or
        // removed on failure) once the POST resolves, matching the
        // spinner→sent→read tick progression the redesign asks for. Appended
        // as a single node so nothing else in the thread flickers.
        const tempId = 'pending-' + (++this._pendingSeq);
        const optimistic = {
            id: tempId, conversation_id: this._activeId, sender_id: App.user.id, sender_name: App.user.full_name,
            body, kind: 'text', attachments: this._pendingAttachments.slice(), created_at: new Date().toISOString(),
            reply_to: replyToId ? { id: ctx.id, sender_name: ctx.senderName, body: ctx.body, kind: ctx.kind } : null,
            _pending: true,
        };
        this._pendingAttachments = [];
        this._composerContext = null;
        if (ta) ta.value = '';
        this.renderPendingAttachments();
        this.renderReplyBar();
        this.autoGrowComposer();
        this.appendMessage(optimistic);

        try {
            const msg = await API.chat.send(this._activeId, body, attachmentIds, 'text', replyToId);
            // Swap the optimistic node for the confirmed one. The WS echo of
            // this same send is deliberately suppressed while a _pending temp
            // is present (see the message.new handler), so appendMessage below
            // is the single authoritative add.
            const idx = this._messages.findIndex(m => m.id === tempId);
            if (idx !== -1) this._messages.splice(idx, 1);
            document.getElementById('chat-msg-' + tempId)?.remove();
            this.appendMessage(msg);
            this.loadConversations();
        } catch (e) {
            const idx = this._messages.findIndex(m => m.id === tempId);
            if (idx !== -1) this._messages.splice(idx, 1);
            document.getElementById('chat-msg-' + tempId)?.remove();
            // Give the user their text back so a transient failure isn't a
            // lost message.
            if (ta && body) { ta.value = body; this.autoGrowComposer(); ta.focus(); }
            UI.toast(e.message, 'error');
        }
    },

    async sendSticker(emoji) {
        this.closeStickerPicker();
        const ctx = this._composerContext;
        const replyToId = ctx && ctx.type === 'reply' ? ctx.id : null;
        try {
            const msg = await API.chat.send(this._activeId, emoji, [], 'sticker', replyToId);
            this._composerContext = null;
            this.renderReplyBar();
            this.appendMessage(msg); // idempotent — safe against the WS echo
            this.loadConversations();
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    toggleStickerPicker() {
        const existing = document.getElementById('chat-sticker-picker');
        if (existing) { existing.remove(); return; }
        const wrap = document.createElement('div');
        wrap.id = 'chat-sticker-picker';
        wrap.className = 'chat-sticker-picker';
        wrap.innerHTML = Object.values(this.STICKERS).map(emojis => `
            <div class="chat-sticker-cat">${emojis.map(e => `<button type="button" class="chat-sticker-option" onclick="ChatPage.sendSticker('${e}')">${e}</button>`).join('')}</div>
        `).join('');
        const composer = document.querySelector('.chat-composer');
        if (composer) composer.appendChild(wrap);
        setTimeout(() => document.addEventListener('click', ChatPage._onDocClickCloseSticker, { once: true, capture: true }), 0);
    },

    closeStickerPicker() {
        document.getElementById('chat-sticker-picker')?.remove();
    },

    _onDocClickCloseSticker(ev) {
        const picker = document.getElementById('chat-sticker-picker');
        if (picker && !picker.contains(ev.target) && !ev.target.closest('.chat-sticker-toggle-btn')) picker.remove();
    },

    deleteMessage(id, forWhom) {
        const title = forWhom === 'me' ? I18N.t('chat.delete_for_me') : I18N.t('chat.delete_for_everyone');
        const body = forWhom === 'me' ? I18N.t('chat.delete_for_me_confirm') : I18N.t('chat.delete_body');
        UI.confirmAction(title, body, I18N.t('common.delete'), async () => {
            try {
                await API.chat.deleteMessage(this._activeId, id, forWhom);
                if (forWhom === 'me') {
                    this.removeMessage(id);
                } else {
                    const m = this._messages.find(x => x.id === id);
                    if (m) { m.deleted_at = new Date().toISOString(); m.body = ''; m.attachments = []; m.reactions = []; this.replaceMessage(m); }
                }
            } catch (e) { UI.toast(e.message, 'error'); }
        });
    },

    /* ── Per-message action menu (reply/copy/forward/edit/delete) ── */

    showMessageMenu(ev, id) {
        ev.preventDefault();
        ev.stopPropagation();
        const m = this._messages.find(x => x.id === id);
        if (!m || m.deleted_at || m._pending) return;
        const mine = m.sender_id === App.user.id;
        const [x, y] = UI.eventPos(ev);
        const items = [
            { action: 'react', label: I18N.t('chat.react'), icon: '😊', handler: () => this.showReactionPickerAt(x, y, id) },
            { action: 'reply', label: I18N.t('chat.reply'), icon: '↩️', handler: () => this.replyToMessage(id) },
        ];
        if (m.kind === 'text' && m.body) {
            items.push({ action: 'copy', label: I18N.t('chat.copy'), icon: '📋', handler: () => this.copyMessageText(m.body) });
        }
        items.push({ action: 'forward', label: I18N.t('chat.forward'), icon: '↪️', handler: () => this.showForwardModal(id) });
        if (mine && m.kind === 'text') {
            items.push({ action: 'edit', label: I18N.t('chat.edit'), icon: '✏️', handler: () => this.startEditMessage(id) });
        }
        items.push({ divider: true });
        items.push({ action: 'delete-me', label: I18N.t('chat.delete_for_me'), icon: '🗑', handler: () => this.deleteMessage(id, 'me') });
        if (mine) {
            items.push({ action: 'delete-everyone', label: I18N.t('chat.delete_for_everyone'), icon: '🗑', danger: true, handler: () => this.deleteMessage(id, 'everyone') });
        }
        UI.showContextMenu(x, y, items);
    },

    /* ── Reactions ── */

    async toggleReaction(messageId, emoji) {
        this.closeReactionPicker();
        try {
            const res = await API.chat.toggleReaction(this._activeId, messageId, emoji);
            const m = this._messages.find(x => x.id === messageId);
            if (m) { m.reactions = res.reactions || []; this.replaceMessage(m); }
        } catch (e) { UI.toast(e.message, 'error'); }
    },

    // A small horizontal emoji row anchored near where the user opened the
    // message menu — pick one to toggle it as a reaction.
    showReactionPickerAt(x, y, messageId) {
        this.closeReactionPicker();
        const wrap = document.createElement('div');
        wrap.id = 'chat-reaction-picker';
        wrap.className = 'chat-reaction-picker';
        wrap.innerHTML = this.REACTIONS.map(e =>
            `<button type="button" class="chat-reaction-pick" onclick="ChatPage.toggleReaction(${messageId},${UI.escJson(e)})">${e}</button>`
        ).join('');
        document.body.appendChild(wrap);
        const w = wrap.offsetWidth || 280, h = wrap.offsetHeight || 44;
        wrap.style.left = Math.max(8, Math.min(x, window.innerWidth - w - 8)) + 'px';
        wrap.style.top = Math.max(8, y - h - 8) + 'px';
        setTimeout(() => document.addEventListener('click', ChatPage._onDocClickCloseReaction, { once: true, capture: true }), 0);
    },

    closeReactionPicker() {
        document.getElementById('chat-reaction-picker')?.remove();
    },

    _onDocClickCloseReaction(ev) {
        const p = document.getElementById('chat-reaction-picker');
        if (p && !p.contains(ev.target)) p.remove();
    },

    copyMessageText(text) {
        if (!navigator.clipboard) return;
        navigator.clipboard.writeText(text).then(
            () => UI.toast(I18N.t('chat.copied'), 'success'),
            () => {},
        );
    },

    /* ── Forward ── */

    showForwardModal(messageId) {
        this._forwardMessageId = messageId;
        this._forwardSelected = new Set();
        const list = this._conversations.map(cv => `
            <label class="chat-forward-item">
                <input type="checkbox" onchange="ChatPage.toggleForwardTarget(${cv.id},this.checked)">
                <span>${UI.esc(this.conversationName(cv))}</span>
            </label>`).join('');
        UI.showModal(I18N.t('chat.forward'), `
            <div class="chat-forward-list">${list || `<p class="text-muted">${I18N.t('chat.no_conversations')}</p>`}</div>`,
            `<button class="btn btn-ghost" onclick="UI.closeModal()">${I18N.t('common.cancel')}</button><button class="btn btn-primary" onclick="ChatPage.confirmForward()">${I18N.t('chat.send')}</button>`);
    },

    toggleForwardTarget(id, checked) {
        if (!this._forwardSelected) return;
        if (checked) this._forwardSelected.add(id);
        else this._forwardSelected.delete(id);
    },

    async confirmForward() {
        if (!this._forwardSelected || !this._forwardSelected.size) {
            UI.toast(I18N.t('chat.forward_select_required'), 'error');
            return;
        }
        try {
            await API.chat.forward(this._forwardMessageId, Array.from(this._forwardSelected));
            UI.closeModal();
            UI.toast(I18N.t('chat.forwarded'), 'success');
            this.loadConversations();
        } catch (e) { UI.toast(e.message, 'error'); }
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
                this._updateMobileClass();
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
            // Someone finishing a message means they're no longer typing.
            if (data.message && data.message.sender_id) this.clearTyping(data.conversation_id, data.message.sender_id);
            if (data.conversation_id === this._activeId) {
                const msg = data.message;
                const mine = msg.sender_id === App.user.id;
                const already = this._messages.some(m => m.id === msg.id);
                // For a message this very tab is sending, an optimistic
                // _pending temp is already on screen and sendMessage() will
                // reconcile it against the POST response — so suppress the WS
                // echo here to avoid briefly showing the temp AND the real one.
                // Other tabs/devices of the same user (no pending temp) still
                // append it live.
                const pendingMine = mine && this._messages.some(m => m._pending);
                if (!already && !pendingMine) this.appendMessage(msg);
                API.chat.markRead(this._activeId).catch(() => {});
            }
            this.loadConversations();
        });
        ChatSocket.on('message.deleted', (data) => {
            if (App.currentPage !== 'chat') return;
            if (data.conversation_id === this._activeId) {
                const m = this._messages.find(x => x.id === data.message_id);
                if (m) { m.deleted_at = new Date().toISOString(); m.body = ''; m.attachments = []; m.reactions = []; this.replaceMessage(m); }
            }
        });
        ChatSocket.on('message.edited', (data) => {
            if (App.currentPage !== 'chat') return;
            if (data.conversation_id === this._activeId) {
                const idx = this._messages.findIndex(x => x.id === data.message.id);
                if (idx !== -1) { this._messages[idx] = data.message; this.replaceMessage(data.message); }
            }
        });
        ChatSocket.on('message.reaction', (data) => {
            if (App.currentPage !== 'chat' || data.conversation_id !== this._activeId) return;
            const m = this._messages.find(x => x.id === data.message_id);
            if (m) { m.reactions = data.reactions || []; this.replaceMessage(m); }
        });
        ChatSocket.on('conversation.read', (data) => {
            if (App.currentPage !== 'chat') return;
            if (data.conversation_id !== this._activeId) return;
            const readAt = new Date(data.last_read_at).getTime();
            this._messages.forEach(m => {
                if (m.sender_id === App.user.id && m.status !== 'read' && new Date(m.created_at).getTime() <= readAt) {
                    m.status = 'read';
                    this.replaceMessage(m);
                }
            });
        });
        ChatSocket.on('conversation.updated', (data) => {
            if (App.currentPage !== 'chat') return;
            this.loadConversations();
            if (data.conversation_id === this._activeId) this.selectConversation(this._activeId);
        });
        // Typing and presence can arrive while on the chat page for any
        // conversation the user is in — the handlers themselves scope updates
        // to the active thread / inbox as needed.
        ChatSocket.on('typing', (data) => { if (App.currentPage === 'chat') this.handleTyping(data); });
        ChatSocket.on('presence', (data) => { if (App.currentPage === 'chat') this.handlePresence(data); });
    },
};
