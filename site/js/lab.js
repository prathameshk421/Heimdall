// Proof lab (Figure 1). An illustration of the real rule: a finding blocks only when its test fails twice on the
// pull request head and passes on the fixed tree. Comments and test output are made up; F1 comes from the summary
// test golden (site/CLAIMS.md). The gate moves through: open -> locked (a proven finding) -> fixed, waiting for a
// person -> approved -> merged.
(function () {
  'use strict';

  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var HEAD = 'a1b2c3d';
  var FIXED = 'e4f5a6b';

  // kind: proven | notrepro | flaky | build | style
  var comments = [
    { who: 'Copilot', what: 'Consider a clearer name for r.', kind: 'style' },
    { who: 'Macroscope', what: 'This call may return a nil client on the error path.', kind: 'notrepro', test: 'TestRefundClientNilOnError' },
    { who: 'Bugbot', what: 'Possible race on the retry counter.', kind: 'flaky', test: 'TestRetryCounterRace' },
    { who: 'CodeRabbit', what: 'A refund runs twice when the retry flag is set, so the customer is paid twice.', where: 'internal/pay/refund.go:42-44', kind: 'proven', test: 'TestRefundRunsOnceWhenRetried' },
    { who: 'CodeRabbit', what: 'Missing error check on Close().', kind: 'build', test: 'TestCloseErrorIsReturned' },
    { who: 'Macroscope', what: 'This log line may expose the refund amount.', kind: 'notrepro', test: 'TestRefundLogHasNoAmount' },
    { who: 'Copilot', what: 'Extract 3 into a named constant.', kind: 'style' },
    { who: 'Bugbot', what: 'The timeout value looks low for this call.', kind: 'notrepro', test: 'TestRefundTimeoutUnderLoad' },
    { who: 'CodeRabbit', what: 'Prefer errors.Is here.', kind: 'style' },
    { who: 'Macroscope', what: 'The parameter ctx is unused.', kind: 'style' },
    { who: 'Copilot', what: 'Add a doc comment to this exported function.', kind: 'style' },
    { who: 'Bugbot', what: 'This loop could be simpler.', kind: 'style' }
  ];

  var $ = function (id) { return document.getElementById(id); };
  var thread = $('thread');
  if (!thread) return;
  var term = $('term');
  var state; // per-run state
  var queue = Promise.resolve();
  var runToken = 0;

  function reset() {
    runToken++;
    queue = Promise.resolve();
    state = { status: comments.map(function () { return 'open'; }), gate: 'open', head: HEAD, busy: false };
    $('labSha').textContent = HEAD;
    term.innerHTML = '<span class="p">heimdall proof (sandbox: no network)\nPick a comment, or press "Prove all 12".</span>\n<span class="cursor"></span>';
    renderThread();
    renderGate();
  }

  function renderThread() {
    thread.innerHTML = '';
    comments.forEach(function (c, i) {
      var li = document.createElement('li');
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'c';
      b.setAttribute('aria-pressed', 'false');
      b.dataset.i = i;
      b.innerHTML = '<span class="lamp"></span><span><span class="who"></span><span class="what"></span>' +
        (c.where ? '<span class="where"></span>' : '') + '</span><span class="st"></span>';
      b.querySelector('.who').textContent = c.who;
      b.querySelector('.what').textContent = c.what;
      if (c.where) b.querySelector('.where').textContent = c.where;
      b.addEventListener('click', function () { enqueue([i]); });
      li.appendChild(b);
      thread.appendChild(li);
      paintRow(i);
    });
  }

  function paintRow(i) {
    var b = thread.querySelector('[data-i="' + i + '"]');
    if (!b) return;
    var s = state.status[i];
    var lamp = b.querySelector('.lamp');
    var st = b.querySelector('.st');
    b.className = 'c';
    lamp.className = 'lamp';
    var label = { open: 'Prove', running: 'Testing', proven: 'Proven, blocks', unproven: 'Unproven, hidden', cleared: 'Fixed on ' + FIXED }[s];
    st.textContent = label;
    if (s === 'running') lamp.classList.add('amber');
    if (s === 'proven') { lamp.classList.add('red'); b.classList.add('done-proven'); }
    if (s === 'unproven') b.classList.add('done-unproven');
    if (s === 'cleared') { lamp.classList.add('green'); b.classList.add('done-cleared'); }
    b.setAttribute('aria-label', comments[i].who + ': ' + comments[i].what + '. ' + label + '.');
  }

  function count(s) { return state.status.filter(function (x) { return x === s; }).length; }

  function renderGate() {
    var lamp = $('gateLamp');
    var text = $('gateText');
    var sub = $('gateSub');
    var actions = $('gateActions');
    lamp.className = 'lamp';
    actions.innerHTML = '';
    $('tProven').textContent = count('proven');
    $('tHidden').textContent = count('unproven');
    $('tOpen').textContent = count('open') + count('running');

    if (state.gate === 'open') {
      if (count('open') + count('running') === comments.length) {
        text.textContent = 'Not checked yet';
        sub.textContent = 'No finding is proven, so nothing blocks. Shadow mode would say the same.';
      } else {
        lamp.classList.add('green');
        text.textContent = 'Nothing proven so far';
        sub.textContent = 'Unproven comments are hidden and counted. They never block the merge.';
      }
    } else if (state.gate === 'locked') {
      lamp.classList.add('red');
      text.textContent = 'Merge locked. F1 is proven.';
      sub.textContent = 'TestRefundRunsOnceWhenRetried failed twice on ' + HEAD + ' and passed on the fix.';
      actions.appendChild(button('Push the fix', function () { enqueueFix(); }));
      actions.appendChild(button('Merge', null, true));
    } else if (state.gate === 'fixing') {
      lamp.classList.add('amber');
      text.textContent = 'New commit. Testing again.';
      sub.textContent = 'The author pushed a fix. Heimdall reruns the proof on the new head.';
    } else if (state.gate === 'approval') {
      lamp.classList.add('amber');
      text.textContent = 'Fixed. Waiting for a person.';
      sub.textContent = 'An approval counts only from someone who is not the author, and only on the head commit, ' + FIXED + '.';
      actions.appendChild(button('Approve ' + FIXED, function () { approve(); }));
      actions.appendChild(button('Merge', null, true));
    } else if (state.gate === 'approved') {
      lamp.classList.add('green');
      text.textContent = 'Gate clear. Approved on ' + FIXED + '.';
      sub.textContent = 'A proven finding was fixed and a person approved this exact commit.';
      actions.appendChild(button('Merge', function () { merge(); }));
    } else if (state.gate === 'merged') {
      lamp.classList.add('green');
      text.textContent = 'Merged ' + FIXED + '.';
      sub.textContent = 'One proven bug fixed before the merge. Eleven comments counted and hidden.';
      actions.appendChild(button('Run it again', function () { reset(); }, false, true));
    }
  }

  function button(label, fn, disabled, line) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn' + (line || disabled ? ' line' : '');
    b.textContent = label;
    if (disabled) { b.disabled = true; b.title = 'Locked until the gate clears'; }
    if (fn) b.addEventListener('click', fn);
    return b;
  }

  // Terminal output.
  function clearCursor() { var c = term.querySelector('.cursor'); if (c) c.remove(); }
  function line(html) {
    clearCursor();
    term.insertAdjacentHTML('beforeend', html + '\n<span class="cursor"></span>');
    term.scrollTop = term.scrollHeight;
  }
  function esc(s) { return String(s).replace(/[&<>"]/g, function (ch) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[ch]; }); }
  function wait(ms, token) {
    return new Promise(function (res, rej) {
      setTimeout(function () { token === runToken ? res() : rej(new Error('reset')); }, reduce ? 0 : ms);
    });
  }

  function proveScript(i, token, fast) {
    var c = comments[i];
    var f = fast ? 0.45 : 1;
    var n = 'C' + (i + 1);
    state.status[i] = 'running';
    paintRow(i);
    renderGate();
    var p = wait(10, token).then(function () {
      line('\n<span class="w">$ heimdall prove ' + n + '</span>  <span class="p">' + esc(c.who) + ': ' + esc(c.what) + '</span>');
    });
    var steps = [];
    if (c.kind === 'style') {
      steps = [
        [260, '<span class="p">  claim: a style or naming request, not a behavior</span>'],
        [320, '<span class="p">  no test can fail on this claim</span>'],
        [200, '  <span class="w">verdict</span> unproven: no test or tool backs it. Hidden and counted.']
      ];
    } else if (c.kind === 'notrepro') {
      steps = [
        [260, '<span class="p">  writing test ' + c.test + '</span>'],
        [420, '  run 1 on ' + HEAD + ' (PR head) ... <span class="g">PASS</span>'],
        [200, '<span class="p">  the test passes on the pull request: not reproduced</span>'],
        [200, '  <span class="w">verdict</span> unproven. Hidden and counted. Unproven does not mean wrong.']
      ];
    } else if (c.kind === 'flaky') {
      steps = [
        [260, '<span class="p">  writing test ' + c.test + '</span>'],
        [420, '  run 1 on ' + HEAD + ' (PR head) ... <span class="r">FAIL</span>'],
        [420, '  run 2 on ' + HEAD + ' (PR head) ... <span class="g">PASS</span>'],
        [200, '<span class="p">  one red run is not proof</span>'],
        [200, '  <span class="w">verdict</span> unproven. Hidden and counted.']
      ];
    } else if (c.kind === 'build') {
      steps = [
        [260, '<span class="p">  writing test ' + c.test + '</span>'],
        [420, '  run 1 on ' + HEAD + ' (PR head) ... <span class="a">BUILD ERROR</span>'],
        [200, '<span class="p">  a build error is not proof</span>'],
        [200, '  <span class="w">verdict</span> unproven. Hidden and counted.']
      ];
    } else {
      steps = [
        [260, '<span class="p">  writing test ' + c.test + '</span>'],
        [480, '  run 1 on ' + HEAD + ' (PR head) ... <span class="r">FAIL</span>  <span class="p">refund ran 2 times, want 1</span>'],
        [480, '  run 2 on ' + HEAD + ' (PR head) ... <span class="r">FAIL</span>  <span class="p">refund ran 2 times, want 1</span>'],
        [480, '  run 3 on the fixed tree    ... <span class="g">PASS</span>'],
        [240, '  <span class="w">verdict</span> <span class="r">PROVEN</span>. F1 ' + esc(c.where) + ' blocks the merge.']
      ];
    }
    steps.forEach(function (s) {
      p = p.then(function () { return wait(s[0] * f, token); }).then(function () { line(s[1]); });
    });
    return p.then(function () {
      state.status[i] = c.kind === 'proven' ? 'proven' : 'unproven';
      if (c.kind === 'proven') state.gate = 'locked';
      paintRow(i);
      renderGate();
      window.dispatchEvent(new CustomEvent('heimdall:lab', { detail: { proven: count('proven'), hidden: count('unproven') } }));
    });
  }

  function enqueue(list, fast) {
    var token = runToken;
    list.forEach(function (i) {
      queue = queue.then(function () {
        if (token !== runToken) return;
        if (state.status[i] !== 'open') return;
        if (state.gate !== 'open' && state.gate !== 'locked') return;
        return proveScript(i, token, fast);
      }).catch(function () {});
    });
  }

  function enqueueFix() {
    var token = runToken;
    queue = queue.then(function () {
      state.gate = 'fixing';
      renderGate();
      line('\n<span class="w">$ git push</span>  <span class="p">author pushes ' + FIXED + ': "Run the refund once on retry"</span>');
      return wait(400, token).then(function () {
        state.head = FIXED;
        $('labSha').textContent = FIXED;
        line('<span class="p">  new head ' + FIXED + '. Every approval on ' + HEAD + ' is void.</span>');
        return wait(380, token);
      }).then(function () {
        line('  rerun TestRefundRunsOnceWhenRetried on ' + FIXED + ' ... <span class="g">PASS</span>');
        return wait(260, token);
      }).then(function () {
        line('  <span class="w">F1</span> <span class="g">fixed</span>. Gate waits for a person.');
        state.status = state.status.map(function (s) { return s === 'proven' ? 'cleared' : s; });
        state.gate = 'approval';
        comments.forEach(function (_, i) { paintRow(i); });
        renderGate();
      });
    }).catch(function () {});
  }

  function approve() {
    line('\n<span class="w">reviewer approves ' + FIXED + '</span>  <span class="p">not the author, on the head commit</span>');
    state.gate = 'approved';
    renderGate();
  }

  function merge() {
    line('<span class="g">  merged ' + FIXED + ' into main</span>');
    state.gate = 'merged';
    renderGate();
  }

  $('proveAll').addEventListener('click', function () {
    enqueue(comments.map(function (_, i) { return i; }), true);
  });
  $('labReset').addEventListener('click', reset);
  thread.addEventListener('click', function (e) {
    var b = e.target.closest('.c');
    if (!b) return;
    thread.querySelectorAll('.c').forEach(function (x) { x.setAttribute('aria-pressed', x === b ? 'true' : 'false'); });
  });
  reset();

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

  // Tabs (manual, with arrow keys).
  var tabs = Array.prototype.slice.call(document.querySelectorAll('[role="tab"]'));
  function select(t) {
    tabs.forEach(function (x) {
      var on = x === t;
      x.setAttribute('aria-selected', on ? 'true' : 'false');
      x.tabIndex = on ? 0 : -1;
      document.getElementById(x.getAttribute('aria-controls')).hidden = !on;
    });
  }
  tabs.forEach(function (t, i) {
    t.addEventListener('click', function () { select(t); });
    t.addEventListener('keydown', function (e) {
      if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
      var n = tabs[(i + (e.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
      select(n); n.focus();
    });
  });

  // Header rule after scroll.
  var top = document.getElementById('top');
  var onScroll = function () { top.classList.toggle('scrolled', window.scrollY > 8); };
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();
})();
