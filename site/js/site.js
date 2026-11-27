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

  // Copy: the visible panel's commands, without comments or "$" prompts.
  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    var label = btn.querySelector('span');
    btn.addEventListener('click', function () {
      var panel = btn.closest('.term').querySelector('[role="tabpanel"]:not([hidden]) code');
      var text = Array.prototype.map.call(panel.childNodes, function (n) {
        return n.nodeType === 1 && (n.classList.contains('c') || n.classList.contains('pr')) ? '' : n.textContent;
      }).join('').split('\n').map(function (l) { return l.replace(/^\s+/, ''); }).filter(Boolean).join('\n');
      var done = function (ok) {
        label.textContent = ok ? 'Copied' : 'Select the text';
        btn.classList.toggle('ok', ok);
        setTimeout(function () { label.textContent = 'Copy'; btn.classList.remove('ok'); }, 1600);
      };
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(false); });
      else done(false);
    });
  });

  // Cards: a soft light follows the pointer.
  document.querySelectorAll('.card').forEach(function (card) {
    card.addEventListener('pointermove', function (e) {
      var r = card.getBoundingClientRect();
      card.style.setProperty('--mx', (e.clientX - r.left) + 'px');
      card.style.setProperty('--my', (e.clientY - r.top) + 'px');
    });
  });

  // Integrations: curved beams from each tool to the hub and from the hub to each chat app.
  var flow = document.getElementById('flow');
  var hub = document.getElementById('hub');
  if (flow && hub) {
    var svg = flow.querySelector('.beams');
    var ns = 'http://www.w3.org/2000/svg';
    var draw = function () {
      var box = flow.getBoundingClientRect();
      var h = hub.getBoundingClientRect();
      var hx = h.left + h.width / 2 - box.left, hy = h.top + h.height / 2 - box.top;
      svg.setAttribute('viewBox', '0 0 ' + box.width + ' ' + box.height);
      svg.innerHTML = '<defs><linearGradient id="beamGrad" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="' + box.width + '" y2="0"><stop offset="0" stop-color="#6b97ff" stop-opacity="0"/><stop offset=".5" stop-color="#6b97ff"/><stop offset="1" stop-color="#ff9670"/></linearGradient></defs>';
      flow.querySelectorAll('[data-beam]').forEach(function (n, i) {
        var r = n.getBoundingClientRect();
        var out = n.dataset.beam === 'out';
        var x = out ? r.left - box.left : r.right - box.left, y = r.top + r.height / 2 - box.top;
        var x1 = out ? hx + h.width / 2 : x, y1 = out ? hy : y, x2 = out ? x : hx - h.width / 2, y2 = out ? y : hy;
        var mx = (x1 + x2) / 2;
        var d = 'M' + x1 + ',' + y1 + ' C' + mx + ',' + y1 + ' ' + mx + ',' + y2 + ' ' + x2 + ',' + y2;
        ['base', 'pulse'].forEach(function (cls) {
          var p = document.createElementNS(ns, 'path');
          p.setAttribute('d', d);
          p.setAttribute('class', cls);
          if (cls === 'pulse') p.style.animationDelay = (i * 0.45) + 's';
          svg.appendChild(p);
        });
      });
    };
    if (window.ResizeObserver) new ResizeObserver(draw).observe(flow);
    window.addEventListener('load', draw);
    draw();
  }

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
