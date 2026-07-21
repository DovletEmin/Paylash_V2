/* Paylash — i18n engine.
   Flat key->string tables per locale live in web/js/locales/*.js (loaded
   as plain <script> tags right before this file, matching the rest of
   this app's no-build-step architecture — each one just sets a global
   window.PAYLASH_LOCALE_XX object). This file only does lookup,
   fallback, interpolation, and persistence. */
const I18N = {
    SUPPORTED: ['en', 'ru', 'tk', 'tr'],
    DATE_LOCALES: { en: 'en-US', ru: 'ru-RU', tk: 'tk-TM', tr: 'tr-TR' },
    STORAGE_KEY: 'paylash-lang',

    dict: {
        en: window.PAYLASH_LOCALE_EN || {},
        ru: window.PAYLASH_LOCALE_RU || {},
        tk: window.PAYLASH_LOCALE_TK || {},
        tr: window.PAYLASH_LOCALE_TR || {},
    },

    lang: 'tk',

    init() {
        this.lang = this.detect();
        document.documentElement.lang = this.lang;
        this._applyDocTitle();
    },

    _applyDocTitle() {
        if (typeof document !== 'undefined' && document.title) document.title = this.t('app.doc_title');
    },

    // localStorage (explicit past choice) wins; otherwise the first of the
    // browser's preferred languages we support; otherwise Turkmen — the
    // app's original, unchanged default for anyone with no signal either way.
    detect() {
        const saved = localStorage.getItem(this.STORAGE_KEY);
        if (saved && this.SUPPORTED.includes(saved)) return saved;
        const browserLangs = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || ''];
        for (const bl of browserLangs) {
            const prefix = String(bl).slice(0, 2).toLowerCase();
            if (this.SUPPORTED.includes(prefix)) return prefix;
        }
        return 'tk';
    },

    setLang(code) {
        if (!this.SUPPORTED.includes(code) || code === this.lang) return;
        this.lang = code;
        localStorage.setItem(this.STORAGE_KEY, code);
        document.documentElement.lang = code;
        this._applyDocTitle();
        // Pre-login screens render outside App's router entirely, so there's
        // no page to re-render into — a reload is the simplest correct way
        // to re-render them in the new language.
        if (typeof App === 'undefined' || !App.currentPage || ['login', 'register'].includes(App.currentPage)) {
            location.reload();
            return;
        }
        App.renderPage(App.currentPage);
    },

    // Looks up key in the active locale, falling back to English, then the
    // raw key itself — never throws, never renders blank, so a missing
    // translation is at least visible/debuggable instead of silently empty.
    t(key, vars) {
        let str = this.dict[this.lang] ? this.dict[this.lang][key] : undefined;
        if (str === undefined && this.dict.en) str = this.dict.en[key];
        if (str === undefined) return key;
        return this._interpolate(str, vars);
    },

    // Pluralized lookup: the locale entry for `key` must be an object like
    // {one:"...", few:"...", many:"...", other:"..."} rather than a plain
    // string. Russian is the only supported locale with real plural
    // categories (1 / 2-4 / 5+, with an 11-14 exception) — en/tk/tr always
    // resolve to `other`, so their entries only need {other: "..."}.
    tn(key, count, vars) {
        let entry = this.dict[this.lang] ? this.dict[this.lang][key] : undefined;
        if (entry === undefined) entry = this.dict.en ? this.dict.en[key] : undefined;
        if (!entry || typeof entry !== 'object') return this.t(key, vars);
        const form = this._pluralForm(count);
        const str = entry[form] !== undefined ? entry[form] : entry.other;
        return this._interpolate(str || '', Object.assign({ count }, vars));
    },

    _pluralForm(n) {
        if (this.lang !== 'ru') return 'other';
        n = Math.abs(n);
        const mod10 = n % 10, mod100 = n % 100;
        if (mod10 === 1 && mod100 !== 11) return 'one';
        if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 'few';
        return 'many';
    },

    _interpolate(str, vars) {
        if (!vars) return str;
        return str.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? vars[k] : m));
    },

    dateLocale() { return this.DATE_LOCALES[this.lang] || 'en-US'; },
};

I18N.init();
