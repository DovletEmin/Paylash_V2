/* Paylash — Auth Pages */
const AuthPage = {
    renderLogin() {
        return `
        <div class="auth-container">
            <div class="auth-card">
                <div class="auth-logo">${UI.icons.cloud} <span>Paylash</span></div>
                <h2 class="auth-title">Ulgama giriň</h2>
                <p class="auth-subtitle">Ulanyjy adyňyzy we parolyňyzy giriziň</p>
                <form id="login-form" class="auth-form">
                    <div class="form-group">
                        <label>Ulanyjy ady</label>
                        <input type="text" id="login-username" class="form-control" placeholder="Ulanyjy adyňyz" required autocomplete="username">
                    </div>
                    <div class="form-group">
                        <label>Parol</label>
                        ${UI.passwordField('login-password', 'Parolyňyz')}
                    </div>
                    <button type="submit" class="btn btn-primary btn-block" id="login-btn">Giriş</button>
                </form>
                <p class="auth-link">Hasabyňyz ýokmy? <a onclick="App.navigate('register')">Hasaba durmak</a></p>
            </div>
        </div>`;
    },

    initLogin() {
        const form = document.getElementById('login-form');
        if (!form) return;
        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const btn = document.getElementById('login-btn');
            btn.disabled = true; btn.textContent = 'Girilýär…';
            try {
                const u = document.getElementById('login-username').value.trim();
                const p = document.getElementById('login-password').value;
                if (!u || !p) { UI.toast('Ähli meýdanlary dolduryň', 'error'); return; }
                await API.auth.login(u, p);
                await App.checkAuth();
                App.navigate('files');
            } catch (err) {
                UI.toast(err.message || 'Giriş ýalňyşlygy', 'error');
            } finally { btn.disabled = false; btn.textContent = 'Giriş'; }
        });
    },

    renderRegister() {
        return `
        <div class="auth-container">
            <div class="auth-card">
                <div class="auth-logo">${UI.icons.cloud} <span>Paylash</span></div>
                <h2 class="auth-title">Hasaba durmak</h2>
                <p class="auth-subtitle">Işgär hökmünde hasap dörediň</p>
                <form id="register-form" class="auth-form">
                    <div class="form-group">
                        <label>Doly ady</label>
                        <input type="text" id="reg-fullname" class="form-control" placeholder="Ady we familiýasy" autocomplete="name">
                    </div>
                    <div class="form-group">
                        <label>Ulanyjy ady</label>
                        <input type="text" id="reg-username" class="form-control" placeholder="Azyndan 3 harp" required autocomplete="username">
                    </div>
                    <div class="form-group">
                        <label>Parol</label>
                        ${UI.passwordField('reg-password', 'Azyndan 6 simwol')}
                    </div>
                    <button type="submit" class="btn btn-primary btn-block" id="register-btn">Hasaba durmak</button>
                </form>
                <p class="auth-link">Hasabyňyz barmy? <a onclick="App.navigate('login')">Ulgama giriň</a></p>
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
            if (username.length < 3) { UI.toast('Ulanyjy ady azyndan 3 harp bolmaly', 'error'); return; }
            if (password.length < 6) { UI.toast('Parol azyndan 6 simwol bolmaly', 'error'); return; }
            btn.disabled = true; btn.textContent = 'Döredilýär…';
            try {
                await API.auth.register(username, password, fullName);
                await API.auth.login(username, password);
                await App.checkAuth();
                UI.toast('Hasabyňyz döredildi', 'success');
                App.navigate('files');
            } catch (err) {
                UI.toast(err.message || 'Hasaba durmak ýalňyşlygy', 'error');
            } finally { btn.disabled = false; btn.textContent = 'Hasaba durmak'; }
        });
    }
};
