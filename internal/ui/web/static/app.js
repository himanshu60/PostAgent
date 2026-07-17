/* Global UI behaviors: double-submit protection, confirm dialogs, toasts,
   datetime guards, stat count-up, alert auto-dismiss. */
(function () {
  'use strict';

  /* ---- Toasts ---- */
  var toastBox = document.createElement('div');
  toastBox.id = 'toast-container';
  document.body.appendChild(toastBox);

  window.showToast = function (msg, kind) {
    var t = document.createElement('div');
    t.className = 'toast' + (kind ? ' toast-' + kind : '');
    t.textContent = msg;
    toastBox.appendChild(t);
    setTimeout(function () {
      t.classList.add('hide');
      setTimeout(function () { t.remove(); }, 450);
    }, 3200);
  };

  /* ---- Confirm dialogs + double-submit protection ----
     Add data-confirm="message" to any form to require confirmation.
     Every submitted form disables its submit buttons and shows a spinner,
     preventing accidental double-publish. */
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (e.defaultPrevented) return;
    // JS-driven forms (no action attribute, e.g. the SSE generate form) manage
    // their own button state.
    if (!form.getAttribute('action')) return;

    var msg = form.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) {
      e.preventDefault();
      return;
    }

    // Disable after this tick so the clicked button's value still submits.
    var buttons = form.querySelectorAll('button[type="submit"], button:not([type])');
    setTimeout(function () {
      buttons.forEach(function (b) {
        b.disabled = true;
        b.classList.add('btn-loading');
      });
    }, 0);
  }, true);

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
