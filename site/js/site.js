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

  // Real captures: tabs that also step on their own until someone picks one.
  var sc = document.getElementById('showcase');
  if (sc) {
    var st = Array.prototype.slice.call(sc.querySelectorAll('[role="tab"]'));
    var pickTab = function (t) {
      st.forEach(function (x) { var on = x === t; x.setAttribute('aria-selected', on); x.tabIndex = on ? 0 : -1; document.getElementById(x.getAttribute('aria-controls')).hidden = !on; });
    };
    var auto = !reduceMotionSc(), idx = 0, scHover = false;
    function reduceMotionSc() { return window.matchMedia('(prefers-reduced-motion: reduce)').matches; }
    st.forEach(function (t, i) {
      t.addEventListener('click', function () { auto = false; idx = i; pickTab(t); });
      t.addEventListener('keydown', function (e) {
        if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
        auto = false; idx = (i + (e.key === 'ArrowRight' ? 1 : st.length - 1)) % st.length; pickTab(st[idx]); st[idx].focus();
      });
    });
    sc.addEventListener('pointerenter', function () { scHover = true; });
    sc.addEventListener('pointerleave', function () { scHover = false; });
    setInterval(function () { if (auto && !scHover && !document.hidden) { idx = (idx + 1) % st.length; pickTab(st[idx]); } }, 5000);
  }

  // Copy: the visible panel's commands, without comments or "$" prompts.
  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    var label = btn.querySelector('span');
    btn.addEventListener('click', function () {
      var panel = btn.closest('[role="tabpanel"]').querySelector('code');
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

  var reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var onScreen = function (el, cb) {
    if (!window.IntersectionObserver) { cb(true); return; }
    new IntersectionObserver(function (es) { cb(es[es.length - 1].isIntersecting); }).observe(el);
  };

  // Story: one ticket from Jira to merge. Each step plays its panel, then the next step starts.
  var story = document.getElementById('story');
  if (story) {
    var beats = Array.prototype.slice.call(story.querySelectorAll('[role="tab"]'));
    var panels = beats.map(function (b) { return document.getElementById(b.getAttribute('aria-controls')); });
    var stState = document.getElementById('stState'), stBar = document.getElementById('stBar');
    var stIc = document.getElementById('stIc'), stLabel = document.getElementById('stLabel'), stCtx = document.getElementById('stCtx');
    var stPanels = story.querySelector('.st-panels');
    // Each step shows the tool it happens in, so the frame above the panel is never just Jira.
    var toolIcon = {
      jira: '<img src="brands/jira.svg" alt="" width="16" height="16">',
      heimdall: '<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M2 3h20v3H2zM4 7.5h6V9H4zM14 7.5h6V9h-6zM5.25 9h3.5v9.5h-3.5zM15.25 9h3.5v9.5h-3.5zM2 19.5h20v2H2z"/></svg>',
      claude: '<img src="brands/claude-ai-icon.svg" alt="" width="16" height="16">',
      lock: '<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3" y="7" width="10" height="7" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" fill="none" stroke="currentColor" stroke-width="1.4"/></svg>',
      github: '<img class="lt" src="brands/github_light.svg" alt="" width="16" height="16"><img class="dk" src="brands/github_dark.svg" alt="" width="16" height="16">'
    };
    var setTool = function (i) {
      var key = panels[i].getAttribute('data-tool') || 'heimdall';
      stIc.innerHTML = toolIcon[key] || toolIcon.heimdall;
      stLabel.textContent = panels[i].getAttribute('data-label');
      stCtx.innerHTML = panels[i].getAttribute('data-ctx');
    };
    var cur = 0, timer = null, thenTimer = null, auto = !reduceMotion, visible = false, hover = false, startedAt = 0, left = 0;
    var stateClass = { 'Merge blocked': 'blocked', 'Merged': 'merged' };
    var show = function (i, focus) {
      cur = i;
      setTool(i);
      beats.forEach(function (b, k) {
        var on = k === i;
        b.setAttribute('aria-selected', on ? 'true' : 'false');
        b.tabIndex = on ? 0 : -1;
        panels[k].classList.toggle('on', on);
        panels[k].classList.remove('play');
        if (on) panels[k].removeAttribute('inert'); else panels[k].setAttribute('inert', '');
      });
      if (!reduceMotion) { void panels[i].offsetWidth; panels[i].classList.add('play'); }
      // The panel is out of flow: give the frame the height of this step.
      var setH = function () {
        var pad = parseFloat(getComputedStyle(stPanels).paddingTop) || 0;
        stPanels.style.height = (panels[i].offsetHeight + pad * 2) + 'px';
      };
      setH();
      if (document.fonts && document.fonts.ready) document.fonts.ready.then(setH);
      var setState = function (s) { stState.textContent = s; stState.className = 'st-state' + (stateClass[s] ? ' ' + stateClass[s] : ''); };
      clearTimeout(thenTimer);
      var then = panels[i].getAttribute('data-then');
      setState(then && reduceMotion ? then : panels[i].getAttribute('data-state'));
      if (then && !reduceMotion) thenTimer = setTimeout(function () { setState(then); }, +panels[i].getAttribute('data-then-at'));
      if (focus) { beats[i].focus(); beats[i].scrollIntoView({ block: 'nearest', inline: 'nearest' }); }
      left = +panels[i].getAttribute('data-dur');
      schedule();
    };
    var stop = function () {
      clearTimeout(timer); timer = null;
      if (startedAt) { left -= Date.now() - startedAt; startedAt = 0; }
      var w = getComputedStyle(stBar).width; stBar.style.transition = 'none'; stBar.style.width = w;
    };
    var schedule = function () {
      clearTimeout(timer); timer = null; startedAt = 0;
      stBar.style.transition = 'none';
      var dur = +panels[cur].getAttribute('data-dur');
      if (!auto) { stBar.style.width = '0'; return; }
      stBar.style.width = (100 * (1 - left / dur)) + '%';
      if (!visible || hover) return;
      void stBar.offsetWidth;
      stBar.style.transition = 'width ' + left + 'ms linear';
      stBar.style.width = '100%';
      startedAt = Date.now();
      timer = setTimeout(function () { show((cur + 1) % beats.length); }, left);
    };
    beats.forEach(function (b, i) {
      b.addEventListener('click', function () { auto = false; show(i); });
      b.addEventListener('keydown', function (e) {
        var k = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[e.key];
        if (e.key === 'Home') k = -i; if (e.key === 'End') k = beats.length - 1 - i;
        if (k === undefined) return;
        e.preventDefault(); auto = false; show((i + k + beats.length) % beats.length, true);
      });
    });
    story.addEventListener('mouseenter', function () { hover = true; if (auto) stop(); });
    story.addEventListener('mouseleave', function () { hover = false; if (auto) schedule(); });
    show(0);
    onScreen(story, function (v) {
      var was = visible; visible = v;
      if (!auto) return;
      if (v && !was) show(cur); else if (!v && was) stop();
    });
  }

  // Tool wheel: the tools ride a large ring; the one at the bottom grows and a signal runs to it from the hub.
  var wheel = document.getElementById('wheel');
  if (wheel) {
    var tools = [
      ['GitHub', 'brands/github_dark.svg', 'brands/github_light.svg', 'Reviews each pull request and posts one check.'],
      ['Slack', 'brands/slack.svg', null, 'Posts proven findings to your channel.'],
      ['OpenRouter', 'brands/openrouter_dark.svg', 'brands/openrouter_light.svg', 'Use any model it serves.'],
      ['Jira', 'brands/jira.svg', null, 'Turns a ticket into failing tests first.'],
      ['Discord', 'brands/discord.svg', null, 'Sends findings and takes commands.'],
      ['Copilot', 'brands/copilot_dark.svg', 'brands/copilot.svg', 'Checks Copilot review comments with the same rule.'],
      ['GitHub Actions', 'brands/github-actions.svg', null, 'Runs inside your CI. No server needed.'],
      ['Telegram', 'brands/telegram.svg', null, 'Findings and commands on your phone.'],
      ['Ollama', 'brands/ollama_dark.svg', 'brands/ollama_light.svg', 'Runs a local model. No key needed.'],
      ['Mattermost', 'brands/mattermost-dark.svg', 'brands/mattermost-light.svg', 'Self-hosted chat, same commands.'],
      ['Bugbot', 'brands/cursor_dark.svg', 'brands/cursor_light.svg', 'Checks Bugbot comments with the same rule.'],
      ['Microsoft Teams', 'brands/microsoft-teams.svg', null, 'Findings where your company talks.'],
      ['DeepSeek', 'brands/deepseek.svg', null, 'A low-cost model that works well.']
    ];
    var n = tools.length, CX = 469, CY = 180, R = 391, SLOT = 25.58, W = 939, H = 780, BIG = 1.735;
    var box = document.getElementById('wheelItems'), glyphs = document.getElementById('wheelGlyphs');
    var lab = document.getElementById('wheelLabel'), comet = document.getElementById('comet'), hubEl = wheel.querySelector('.wheel-hub');
    var els = [];
    var pick = function (t) { return document.documentElement.classList.contains('is-dark') ? t[1] : (t[2] || t[1]); };
    for (var k = -6; k <= 6; k++) { var d = document.createElement('span'); d.className = 'tool'; d.innerHTML = '<img alt="">'; box.appendChild(d); els.push(d); }
    for (var g = 0; g < 7; g++) { var s = document.createElement('span'); glyphs.appendChild(s); }
    var rot = 0, from = 0, to = 0, start = 0, dur = 480, last = performance.now(), nextAt = 2200, clock = 0, visible = true, hover = false;
    var ease = function (x) { return x < .5 ? 2 * x * x : 1 - Math.pow(-2 * x + 2, 2) / 2; };
    var setLabel = function (i) { var t = tools[((i % n) + n) % n]; lab.firstChild.textContent = t[0]; lab.lastChild.textContent = t[3]; };
    setLabel(0);
    var frame = function (now) {
      var dt = now - last; last = now;
      if (visible && !hover && !reduceMotion) clock += dt;
      if (clock >= nextAt) { from = rot; to = Math.round(rot) + 1; start = clock; nextAt = clock + 2200; setTimeout(function () { setLabel(to); }, dur * .5); }
      var p = Math.min(1, (clock - start) / dur);
      rot = from + (to - from) * (start ? ease(p) : 1);
      var base = Math.round(rot);
      els.forEach(function (el, idx) {
        var j = base + idx - 6;
        var ang = (90 + (j - rot) * SLOT) * Math.PI / 180;
        var x = CX + R * Math.cos(ang), y = CY + R * Math.sin(ang);
        var dist = Math.abs(j - rot);
        var sc = 1 + (BIG - 1) * Math.max(0, 1 - dist * 1.6);
        var kk = Math.min(box.clientWidth / W, box.clientHeight / H), ox = (box.clientWidth - W * kk) / 2, oy = box.clientHeight - H * kk;
        el.style.transform = 'translate(' + (ox + x * kk) + 'px,' + (oy + y * kk) + 'px) scale(' + sc.toFixed(3) + ')';
        el.style.width = el.style.height = (72 * kk) + 'px'; el.style.margin = (-36 * kk) + 'px 0 0 ' + (-36 * kk) + 'px';
        el.style.opacity = y > H + 40 ? 0 : 1;
        var t = tools[((j % n) + n) % n];
        var img = el.firstChild, src = pick(t);
        if (img.getAttribute('src') !== src) img.setAttribute('src', src);
        el.style.zIndex = dist < .5 ? 2 : 1;
      });
      var kh = Math.min(box.clientWidth / W, box.clientHeight / H);
      hubEl.style.width = hubEl.style.height = (125 * kh) + 'px';
      hubEl.style.left = ((box.clientWidth - W * kh) / 2 + 469 * kh) + 'px';
      hubEl.style.top = (box.clientHeight - H * kh + 347 * kh) + 'px';
      // Signal from the hub down to the focus slot, once per step.
      var phase = ((clock % 2200) / 2200);
      var y1 = 410 + phase * 110;
      comet.setAttribute('y1', Math.max(410, y1 - 34)); comet.setAttribute('y2', Math.min(520, y1));
      comet.style.opacity = phase < .85 ? 1 : 0;
      // Code glyphs around the focus slot.
      Array.prototype.forEach.call(glyphs.children, function (sp, gi) {
        var a = (gi * 51 + clock * .02) * Math.PI / 180, rr = 74 + (gi % 3) * 8;
        var kq = Math.min(glyphs.clientWidth / W, glyphs.clientHeight / H), oxq = (glyphs.clientWidth - W * kq) / 2, oyq = glyphs.clientHeight - H * kq;
        sp.style.left = (oxq + (469 + rr * Math.cos(a)) * kq) + 'px';
        sp.style.top = (oyq + (571 + rr * Math.sin(a)) * kq) + 'px';
        var on = Math.sin(clock / 300 + gi * 1.7) > .35;
        sp.style.opacity = on ? .9 : 0;
        if (on && Math.random() < .04) sp.textContent = '{}[]()<>/*@%&#:;+=01'.charAt(Math.floor(Math.random() * 20));
      });
      requestAnimationFrame(frame);
    };
    onScreen(wheel, function (v) { visible = v; });
    wheel.addEventListener('pointerenter', function () { hover = true; });
    wheel.addEventListener('pointerleave', function () { hover = false; });
    requestAnimationFrame(frame);
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
