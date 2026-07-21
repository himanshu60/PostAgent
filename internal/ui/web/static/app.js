/* Global UI behaviors: double-submit protection, confirm dialogs, toasts,
   datetime guards, stat count-up, alert auto-dismiss. */
(function () {
  'use strict';

  /* ---- Toasts ---- */
  var toastBox = document.createElement('div');
  toastBox.id = 'toast-container';
  document.body.appendChild(toastBox);

  window.showToast = function (msg, kind, duration) {
    var t = document.createElement('div');
    t.className = 'toast' + (kind ? ' toast-' + kind : '');
    t.textContent = msg;
    toastBox.appendChild(t);
    setTimeout(function () {
      t.classList.add('hide');
      setTimeout(function () { t.remove(); }, 450);
    }, duration || 3400);
  };

  function escapeHtml(s) {
    var d = document.createElement('div');
    d.appendChild(document.createTextNode(s == null ? '' : String(s)));
    return d.innerHTML;
  }

  /* ---- Premium confirm modal (replaces window.confirm) ---- */
  window.premiumConfirm = function (message) {
    return new Promise(function (resolve) {
      var back = document.createElement('div');
      back.className = 'modal-backdrop show';
      back.innerHTML =
        '<div class="modal-dialog" role="dialog" aria-modal="true">' +
          '<div style="font-size:17px;font-weight:700;margin-bottom:10px;color:var(--text);">Please confirm</div>' +
          '<div style="font-size:14px;color:var(--text-muted);margin-bottom:22px;line-height:1.55;">' + escapeHtml(message) + '</div>' +
          '<div style="display:flex;gap:10px;justify-content:center;">' +
            '<button type="button" class="btn btn-outline" data-act="cancel">Cancel</button>' +
            '<button type="button" class="btn btn-primary" data-act="ok">Confirm</button>' +
          '</div>' +
        '</div>';
      document.body.appendChild(back);
      function done(v) { back.remove(); document.removeEventListener('keydown', onKey); resolve(v); }
      function onKey(ev) { if (ev.key === 'Escape') done(false); else if (ev.key === 'Enter') done(true); }
      document.addEventListener('keydown', onKey);
      back.addEventListener('click', function (ev) {
        if (ev.target === back) return done(false);
        var act = ev.target.closest('[data-act]');
        if (act) done(act.getAttribute('data-act') === 'ok');
      });
      setTimeout(function () { var b = back.querySelector('[data-act="ok"]'); if (b) b.focus(); }, 30);
    });
  };

  function disableFormButtons(form) {
    form.querySelectorAll('button[type="submit"], button:not([type])').forEach(function (b) {
      b.disabled = true; b.classList.add('btn-loading');
    });
  }

  /* ---- Confirm dialogs + double-submit protection ----
     Add data-confirm="message" to any form to require confirmation via a
     premium modal. Every submitted form disables its buttons to prevent
     accidental double-submits. */
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (e.defaultPrevented) return;

    // data-confirm may live on the form OR on the specific submit button.
    var msg = form.getAttribute('data-confirm') || (e.submitter && e.submitter.getAttribute('data-confirm'));
    if (msg && form.dataset.confirmed !== '1') {
      e.preventDefault();
      window.premiumConfirm(msg).then(function (ok) {
        if (ok) { form.dataset.confirmed = '1'; disableFormButtons(form); form.submit(); }
      });
      return;
    }
    // JS-driven forms (no action attribute) manage their own button state.
    if (!form.getAttribute('action')) return;
    setTimeout(function () { disableFormButtons(form); }, 0);
  }, true);

  /* ---- Flash messages as toasts (from ?ok / ?err / ?autopost) ---- */
  (function () {
    var p = new URLSearchParams(location.search);
    var ok = p.get('ok'), err = p.get('err'), ap = p.get('autopost');
    var shown = false;
    if (ap === 'enabled') {
      showToast('⚡ Auto-post is ON! Every publish day we research a topic from your interests, write a post, and publish it to your LinkedIn automatically. Your first one is being generated now — check the Scheduled tab in a minute.', 'success', 9000);
      shown = true;
    } else if (ap === 'disabled') {
      showToast('Auto-post stopped. You are back to manual — generate posts yourself anytime.', null, 5000);
      shown = true;
    }
    if (ok) { showToast(ok, 'success', 5000); shown = true; }
    if (err) { showToast(err, 'error', 6000); shown = true; }
    if (shown) {
      p.delete('ok'); p.delete('err'); p.delete('autopost');
      var qs = p.toString();
      history.replaceState(null, '', location.pathname + (qs ? '?' + qs : '') + location.hash);
    }
  })();

  /* ---- datetime-local: block past times client-side ---- */
  function pad(n) { return (n < 10 ? '0' : '') + n; }
  function nowLocalString() {
    var d = new Date();
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
      'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }
  document.querySelectorAll('input[type="datetime-local"]').forEach(function (inp) {
    inp.min = nowLocalString();
  });

  /* ---- Stat count-up animation ---- */
  document.querySelectorAll('.stat-value').forEach(function (el) {
    var target = parseInt(el.textContent.trim(), 10);
    if (isNaN(target) || target === 0) return;
    var dur = 700, start = null;
    el.textContent = '0';
    function step(ts) {
      if (!start) start = ts;
      var p = Math.min((ts - start) / dur, 1);
      // ease-out cubic
      var eased = 1 - Math.pow(1 - p, 3);
      el.textContent = Math.round(eased * target);
      if (p < 1) requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
  });

  /* ---- Auto-dismiss flash alerts ---- */
  document.querySelectorAll('.alert').forEach(function (al) {
    setTimeout(function () {
      al.classList.add('fade-out');
      setTimeout(function () { al.remove(); }, 550);
    }, 5000);
  });

  /* ---- Collapsible toggle (reschedule panels) ---- */
  window.toggleCollapse = function (id) {
    var el = document.getElementById(id);
    if (el) el.classList.toggle('open');
  };

  /* ---- Card cursor spotlight (Linear/Vercel-style micro-interaction) ---- */
  var spotlightCards = document.querySelectorAll('.card');
  if (window.matchMedia && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    spotlightCards.forEach(function (card) {
      card.addEventListener('mousemove', function (e) {
        var r = card.getBoundingClientRect();
        card.style.setProperty('--mx', (e.clientX - r.left) + 'px');
        card.style.setProperty('--my', (e.clientY - r.top) + 'px');
      });
    });
  }

  /* ---- Graceful media image loading ----
     Generated images (e.g. Pollinations) render on demand and can be slow or
     occasionally fail. Show a skeleton while loading and a clean placeholder on
     error, instead of the browser's broken-image glyph. */
  document.querySelectorAll('img.media-preview-img').forEach(function (img) {
    img.loading = 'lazy';
    if (!img.complete) img.classList.add('media-loading');
    img.addEventListener('load', function () { img.classList.remove('media-loading'); });
    img.addEventListener('error', function () {
      var ph = document.createElement('div');
      ph.className = 'media-fallback';
      ph.textContent = 'Preview image is still generating or unavailable — it will still be attached when the post is published.';
      img.replaceWith(ph);
    });
  });

  /* ---- Mobile nav toggle ---- */
  var navToggle = document.getElementById('nav-toggle');
  var navLinks = document.getElementById('nav-links');
  if (navToggle && navLinks) {
    navToggle.addEventListener('click', function (e) {
      e.stopPropagation();
      var open = navLinks.classList.toggle('open');
      navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
    // Tap outside closes the menu.
    document.addEventListener('click', function (e) {
      if (navLinks.classList.contains('open') && !navLinks.contains(e.target) && e.target !== navToggle) {
        navLinks.classList.remove('open');
        navToggle.setAttribute('aria-expanded', 'false');
      }
    });
  }

  /* ---- User menu dropdown ---- */
  var userBtn = document.getElementById('user-btn');
  var userDropdown = document.getElementById('user-dropdown');
  if (userBtn && userDropdown) {
    userBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      var open = userDropdown.classList.toggle('open');
      userBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
    document.addEventListener('click', function (e) {
      if (userDropdown.classList.contains('open') && !userDropdown.contains(e.target) && e.target !== userBtn) {
        userDropdown.classList.remove('open');
        userBtn.setAttribute('aria-expanded', 'false');
      }
    });
  }

  /* ---- Dark/light theme toggle ---- */
  var themeBtn = document.getElementById('theme-toggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('pa-theme', next);
    });
  }
})();
