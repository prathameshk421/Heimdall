// Hero depth and the "try to cheat the tests" widget. No library.
(function () {
  'use strict';
  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

  // Hero: the far hills, the gate and the near mist move at different depths with the pointer.
  var art = document.getElementById('heroArt');
  var hero = document.querySelector('.hero');
  if (art && hero && window.matchMedia('(hover: hover)').matches) {
    var layers = Array.prototype.slice.call(art.querySelectorAll('.layer'));
    var raf = 0, px = 0, py = 0;
    var apply = function () {
      raf = 0;
      layers.forEach(function (l) {
        var d = parseFloat(l.getAttribute('data-depth')) || 0;
        l.style.transform = 'translate(' + (px * d * 10).toFixed(2) + 'px,' + (py * d * 5).toFixed(2) + 'px)';
      });
    };
    hero.addEventListener('pointermove', function (e) {
      if (reduce.matches) return;
      var r = hero.getBoundingClientRect();
      px = (e.clientX - r.left) / r.width * 2 - 1;
      py = (e.clientY - r.top) / r.height * 2 - 1;
      if (!raf) raf = requestAnimationFrame(apply);
    });
    hero.addEventListener('pointerleave', function () { px = 0; py = 0; if (!raf) raf = requestAnimationFrame(apply); });
  }

  // Try to cheat the tests. Rules and levels from internal/domain/testintegrity (doc.go); diffs are examples.
  var moves = [
    {
      label: 'Edit the pinned test until it passes', level: 'block', rule: 'pinned-test-edited',
      diff: [['p', 'refund_test.go (pinned)'], ['del', '-	if got != 1 {'], ['add', '+	if got > 2 {'], ['p', '  	t.Fatalf("refund ran %d times, want 1", got)']],
      title: 'Blocked',
      body: 'Pinned tests are read-only. Any edit, delete or move of a pinned test blocks the merge until a policy owner approves the head commit.'
    },
    {
      label: 'Skip the failing test', level: 'warn', rule: 'skip-added',
      diff: [['p', 'refund_test.go'], ['p', '  func TestRefundRunsOnceWhenRetried(t *testing.T) {'], ['add', '+	t.Skip("flaky on CI")']],
      title: 'Flagged',
      body: 'A new skip is named on the check: t.Skip, pytest skip and xfail, it.skip, describe.skip, @Disabled, #[ignore] and more. In a file pinned as a whole, it blocks.'
    },
    {
      label: 'Delete the assertion', level: 'warn', rule: 'assertion-deleted',
      diff: [['p', 'refund_test.go'], ['del', '-	if got != 1 {'], ['del', '-		t.Fatalf("refund ran %d times, want 1", got)'], ['del', '-	}']],
      title: 'Flagged',
      body: 'When a change removes more assertions than it adds, the check says so and names the test.'
    },
    {
      label: 'Assert something that cannot fail', level: 'warn', rule: 'tautological-assertion',
      diff: [['p', 'refund.test.ts'], ['del', '-  expect(refunds(order)).toBe(1)'], ['add', '+  expect(true).toBe(true)']],
      title: 'Flagged',
      body: 'An added assertion on a constant, or one that compares a value with itself, is a tautology. It cannot catch a bug, so the check names it.'
    },
    {
      label: 'Mock the code you just changed', level: 'warn', rule: 'mocks-changed-file',
      diff: [['p', 'refund.test.ts'], ['add', "+jest.mock('../src/refund')"], ['p', '  // src/refund.ts is edited by this same pull request']],
      title: 'Flagged',
      body: 'A test that mocks a file the same change edits tests the mock, not the code. Go fakes, gomock, Python patch targets, jest.mock and vi.mock are read.'
    },
    {
      label: 'Raise the timeout', level: 'warn', rule: 'timeout-loosened',
      diff: [['p', 'refund_test.go'], ['del', '-	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)'], ['add', '+	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)']],
      title: 'Flagged',
      body: 'A timeout number that grows is named on the check, with Go time units and ms, s, m and h suffixes. A raised retry count is caught the same way.'
    },
    {
      label: 'Rerun until it goes green', level: 'note', rule: 'flaky',
      diff: [['p', 'heimdall proof'], ['p', '  run 1 on a1b2c3d ... FAIL'], ['p', '  run 2 on a1b2c3d ... PASS']],
      title: 'Not proof, not a pass',
      body: 'Results that differ on one tree are flaky. A rerun that passes is never a clean pass, and one red run is never proof.'
    }
  ];

  var list = document.getElementById('moves');
  var panel = document.getElementById('verdict');
  if (!list || !panel) return;
  var tagText = { block: 'blocks', warn: 'warns', note: 'not proof' };

  function show(i) {
    var m = moves[i];
    Array.prototype.forEach.call(list.children, function (b, j) {
      b.setAttribute('aria-selected', j === i ? 'true' : 'false');
      b.tabIndex = j === i ? 0 : -1;
    });
    panel.setAttribute('aria-labelledby', 'move-' + i);
    panel.innerHTML = '';
    var pre = document.createElement('pre');
    m.diff.forEach(function (d, k) {
      var span = document.createElement('span');
      span.className = d[0];
      span.textContent = d[1] + (k < m.diff.length - 1 ? '\n' : '');
      pre.appendChild(span);
    });
    var res = document.createElement('div');
    res.className = 'res';
    var dot = document.createElement('span');
    dot.className = 'dot ' + (m.level === 'note' ? '' : m.level);
    var t = document.createElement('span');
    t.textContent = m.title + ' ';
    var c = document.createElement('code');
    c.textContent = m.rule;
    res.append(dot, t, c);
    var p = document.createElement('p');
    p.textContent = m.body;
    panel.append(pre, res, p);
  }

  moves.forEach(function (m, i) {
    var b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('role', 'tab');
    b.id = 'move-' + i;
    b.setAttribute('aria-controls', 'verdict');
    var l = document.createElement('span');
    l.textContent = m.label;
    var tag = document.createElement('span');
    tag.className = 'tag' + (m.level === 'block' ? ' block' : '');
    tag.textContent = tagText[m.level];
    b.append(l, tag);
    b.addEventListener('click', function () { show(i); });
    b.addEventListener('keydown', function (e) {
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      var n = (i + (e.key === 'ArrowDown' ? 1 : moves.length - 1)) % moves.length;
      show(n);
      list.children[n].focus();
    });
    list.appendChild(b);
  });
  show(0);
})();
