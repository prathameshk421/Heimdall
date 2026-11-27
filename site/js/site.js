// Header state, setup tabs, copy buttons, GitHub star count and the demo dialog. No library.
(function () {
  'use strict';

  // Header: clear over the painting, solid once the page scrolls.
  var top = document.getElementById('siteTop');
  if (top) {
    var onScroll = function () { top.classList.toggle('scrolled', window.scrollY > 24); };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
  }

  // Theme: follows the system until the visitor picks one; the choice is remembered.
  var root = document.documentElement;
  var media = window.matchMedia('(prefers-color-scheme: dark)');
  var themeBtn = document.getElementById('themeBtn');
  var isDark = function () { return root.dataset.theme ? root.dataset.theme === 'dark' : media.matches; };
  var paint = function () {
    var d = isDark();
    root.classList.toggle('is-dark', d);
    if (themeBtn) themeBtn.setAttribute('aria-label', d ? 'Switch to light theme' : 'Switch to dark theme');
  };
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      root.dataset.theme = isDark() ? 'light' : 'dark';
      try { localStorage.setItem('theme', root.dataset.theme); } catch (e) {}
      paint();
    });
  }
  if (media.addEventListener) media.addEventListener('change', paint);
  paint();

  // Setup tabs, with arrow keys.
  var tabs = Array.prototype.slice.call(document.querySelectorAll('.tabs [role="tab"]'));
  var select = function (t) {
    tabs.forEach(function (x) {
      var on = x === t;
      x.setAttribute('aria-selected', on ? 'true' : 'false');
      x.tabIndex = on ? 0 : -1;
      document.getElementById(x.getAttribute('aria-controls')).hidden = !on;
    });
  };
  tabs.forEach(function (t, i) {
    t.addEventListener('click', function () { select(t); });
    t.addEventListener('keydown', function (e) {
      if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
      var n = tabs[(i + (e.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
      select(n);
      n.focus();
    });
  });

  // Copy buttons.
  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var code = btn.parentElement.querySelector('code').textContent;
      var done = function (ok) {
        btn.textContent = ok ? 'Copied' : 'Select';
        btn.classList.toggle('ok', ok);
        setTimeout(function () { btn.textContent = 'Copy'; btn.classList.remove('ok'); }, 1600);
      };
      if (navigator.clipboard) navigator.clipboard.writeText(code).then(function () { done(true); }, function () { done(false); });
      else done(false);
    });
  });

  // Demo video in a dialog. Closing it pauses the video.
  var dialog = document.getElementById('demo');
  var open = document.getElementById('openDemo');
  if (dialog && open && typeof dialog.showModal === 'function') {
    var video = dialog.querySelector('video');
    open.addEventListener('click', function () {
      dialog.showModal();
      video.play().catch(function () {});
    });
    dialog.querySelector('.demo-x').addEventListener('click', function () { dialog.close(); });
    dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
    dialog.addEventListener('close', function () { video.pause(); });
  } else if (open) {
    open.addEventListener('click', function () { window.location.href = 'media/heimdall-demo.mp4'; });
  }

  // GitHub star count on the nav button. Hidden unless the API answers.
  var stars = document.querySelector('[data-stars]');
  if (stars && window.fetch) {
    fetch('https://api.github.com/repos/prathameshk421/Heimdall', { headers: { Accept: 'application/vnd.github+json' } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) {
        if (!d || typeof d.stargazers_count !== 'number') return;
        var n = d.stargazers_count;
        stars.textContent = n >= 1000 ? (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k' : String(n);
        stars.hidden = false;
        stars.parentElement.setAttribute('aria-label', 'GitHub, ' + n + ' stars');
      })
      .catch(function () {});
  }
})();
