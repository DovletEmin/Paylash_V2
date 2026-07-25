/* Paylash — Auth Pages */
const AuthPage = {
    renderLogin() {
        return `
        <div class="auth-container">
            <div class="auth-lang-switcher">${UI.langSwitcher()}</div>
            <div class="auth-card">
                <div class="auth-logo">${UI.icons.cloud} <span>Paýlaş</span></div>
                <h2 class="auth-title">${I18N.t('auth.login_title')}</h2>
                <p class="auth-subtitle">${I18N.t('auth.login_subtitle')}</p>
                <form id="login-form" class="auth-form">
                    <div class="form-group">
                        <label>${I18N.t('auth.username_label')}</label>
                        <input type="text" id="login-username" class="form-control" placeholder="${I18N.t('auth.username_placeholder')}" required autocomplete="username">
                    </div>
                    <div class="form-group">
                        <label>${I18N.t('auth.password_label')}</label>
                        ${UI.passwordField('login-password', I18N.t('auth.password_placeholder'))}
                    </div>
                    <button type="submit" class="btn btn-primary btn-block" id="login-btn">${I18N.t('auth.login_button')}</button>
                </form>
                ${App.config.allow_registration ? `<p class="auth-link">${I18N.t('auth.no_account')} <a onclick="App.navigate('register')">${I18N.t('auth.register_link')}</a></p>` : ''}
            </div>
        </div>`;
    },

    initLogin() {
        const form = document.getElementById('login-form');
        if (!form) return;
        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const btn = document.getElementById('login-btn');
            btn.disabled = true; btn.textContent = I18N.t('auth.login_loading');
            try {
                const u = document.getElementById('login-username').value.trim();
                const p = document.getElementById('login-password').value;
                if (!u || !p) { UI.toast(I18N.t('auth.fill_all_fields'), 'error'); return; }
                await API.auth.login(u, p);
                await App.checkAuth();
                App.navigate('files');
                App.checkForcedPasswordChange();
            } catch (err) {
                UI.toast(err.message || I18N.t('auth.login_error'), 'error');
            } finally { btn.disabled = false; btn.textContent = I18N.t('auth.login_button'); }
        });
    },

    renderRegister() {
        return `
        <div class="auth-container">
            <div class="auth-lang-switcher">${UI.langSwitcher()}</div>
            <div class="auth-card">
                <div class="auth-logo">${UI.icons.cloud} <span>Paýlaş</span></div>
                <h2 class="auth-title">${I18N.t('auth.register_title')}</h2>
                <p class="auth-subtitle">${I18N.t('auth.register_subtitle')}</p>
                <form id="register-form" class="auth-form">
                    <div class="form-group">
                        <label>${I18N.t('auth.fullname_label')}</label>
                        <input type="text" id="reg-fullname" class="form-control" placeholder="${I18N.t('auth.fullname_placeholder')}" autocomplete="name">
                    </div>
                    <div class="form-group">
                        <label>${I18N.t('auth.username_label')}</label>
                        <input type="text" id="reg-username" class="form-control" placeholder="${I18N.t('auth.username_min_placeholder')}" required autocomplete="username">
                    </div>
                    <div class="form-group">
                        <label>${I18N.t('auth.password_label')}</label>
                        ${UI.passwordField('reg-password', I18N.t('auth.password_min_placeholder'))}
                    </div>
                    <button type="submit" class="btn btn-primary btn-block" id="register-btn">${I18N.t('auth.register_button')}</button>
                </form>
                <p class="auth-link">${I18N.t('auth.have_account')} <a onclick="App.navigate('login')">${I18N.t('auth.login_link')}</a></p>
            </div>
        </div>`;
    },

    initRegister() {
        const form = document.getElementById('register-form');
        if (!form) return;
        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const btn = document.getElementById('register-btn');
            const fullName = document.getElementById('reg-fullname').value.trim();
            const username = document.getElementById('reg-username').value.trim();
            const password = document.getElementById('reg-password').value;
            if (username.length < 3) { UI.toast(I18N.t('auth.username_too_short'), 'error'); return; }
            if (password.length < 6) { UI.toast(I18N.t('auth.password_too_short'), 'error'); return; }
            btn.disabled = true; btn.textContent = I18N.t('auth.register_loading');
            try {
                await API.auth.register(username, password, fullName);
                await API.auth.login(username, password);
                await App.checkAuth();
                UI.toast(I18N.t('auth.register_success'), 'success');
                App.navigate('files');
            } catch (err) {
                UI.toast(err.message || I18N.t('auth.register_error'), 'error');
            } finally { btn.disabled = false; btn.textContent = I18N.t('auth.register_button'); }
        });
    }
};
