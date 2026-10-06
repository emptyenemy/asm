// Install tabs, copy buttons, section shortcuts and the ASCII field behind
// the banner. The terminal replay lives in demo.js.
(function () {
  'use strict';

  // ---- install tabs ----------------------------------------------------

  var tabs = Array.prototype.slice.call(document.querySelectorAll('[role="tab"][data-os]'));
  var panel = document.getElementById('install-panel');

  function detectOS() {
    var platform = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '';
    var agent = navigator.userAgent || '';
    if (/win/i.test(platform) || /Windows/.test(agent)) return 'windows';
    if (/mac|iphone|ipad/i.test(platform) || /Mac OS X/.test(agent)) return 'macos';
    if (/linux|x11|android/i.test(platform + agent)) return 'linux';
    return 'windows';
  }

  function select(os, focus) {
    var family = os === 'windows' ? 'windows' : 'unix';
    tabs.forEach(function (tab) {
      var on = tab.dataset.os === os;
      tab.setAttribute('aria-selected', on ? 'true' : 'false');
      tab.tabIndex = on ? 0 : -1;
      if (on) {
        panel.setAttribute('aria-labelledby', tab.id);
        if (focus) tab.focus();
      }
    });
    panel.querySelectorAll('[data-for]').forEach(function (node) {
      node.hidden = node.dataset.for !== family;
    });
    document.dispatchEvent(new CustomEvent('asm:os', { detail: os }));
  }

  tabs.forEach(function (tab, index) {
    tab.addEventListener('click', function () { select(tab.dataset.os, false); });
    tab.addEventListener('keydown', function (event) {
      var step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
      if (event.key === 'Home') step = -index;
      if (event.key === 'End') step = tabs.length - 1 - index;
      if (step === undefined) return;
      event.preventDefault();
      select(tabs[(index + step + tabs.length) % tabs.length].dataset.os, true);
    });
  });

  window.asmOS = detectOS();
  select(window.asmOS, false);

  // ---- copy ------------------------------------------------------------

  // The clipboard API needs a secure context, so a local file:// preview
  // falls back to a hidden textarea.
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    var area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    try { document.execCommand('copy'); } catch (error) { /* nothing useful to report */ }
    document.body.removeChild(area);
    return Promise.resolve();
  }

  document.querySelectorAll('.copy').forEach(function (button) {
    var timer;
    button.addEventListener('click', function () {
      var text = document.getElementById(button.dataset.copy).textContent;
      var done = function () {
        button.textContent = 'copied';
        button.dataset.done = '';
        clearTimeout(timer);
        timer = setTimeout(function () {
          button.textContent = 'copy';
          delete button.dataset.done;
        }, 1600);
      };
      copyText(text).then(done, done);
    });
  });

  // ---- section shortcuts: 1-5, like the function keys of a TUI menu ------

  var keyed = {};
  document.querySelectorAll('nav a[data-key]').forEach(function (link) {
    keyed[link.dataset.key] = link;
  });
  document.addEventListener('keydown', function (event) {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    var target = event.target;
    if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
    var link = keyed[event.key];
    if (!link) return;
    event.preventDefault();
    var section = document.querySelector(link.getAttribute('href'));
    section.scrollIntoView();
    history.replaceState(null, '', link.getAttribute('href'));
  });

  // ---- the field behind the banner ---------------------------------------

  // Bands of characters drifting across the hero like moving air, densest
  // to the right of the banner and fading toward the edges. Static: the
  // terminal below is the only thing that moves on its own.
  var field = document.querySelector('.field');
  var ramp = '  ..`-~~:=+*';

  function noise(x, y) {
    var n = Math.sin(x * 12.9898 + y * 78.233) * 43758.5453;
    return n - Math.floor(n);
  }

  function drawField() {
    if (!field) return;
    var probe = document.createElement('span');
    probe.textContent = 'MMMMMMMMMM';
    field.textContent = '';
    field.appendChild(probe);
    var cell = probe.getBoundingClientRect().width / 10 || 7.2;
    var line = parseFloat(getComputedStyle(field).lineHeight) || 14;
    var cols = Math.ceil(field.clientWidth / cell);
    var rows = Math.ceil(field.clientHeight / line);
    var wide = field.clientWidth > 720;
    var cx = cols * (wide ? 0.74 : 0.6);
    var cy = rows * (wide ? 0.2 : 0.16);
    var rx = cols * (wide ? 0.42 : 0.75);
    var ry = rows * (wide ? 0.32 : 0.22);
    var out = [];
    for (var y = 0; y < rows; y++) {
      var row = '';
      for (var x = 0; x < cols; x++) {
        var dx = (x - cx) / rx;
        var dy = (y - cy) / ry;
        var fall = Math.max(0, 1 - Math.sqrt(dx * dx + dy * dy));
        var wave = Math.sin(x * 0.11 + Math.sin(y * 0.33 + x * 0.025) * 2.2 - y * 0.5);
        var v = fall * (0.55 + 0.45 * wave) + (noise(x, y) - 0.5) * 0.18 * fall;
        var index = Math.max(0, Math.min(ramp.length - 1, Math.floor(v * ramp.length * 1.1)));
        row += ramp.charAt(index);
      }
      out.push(row);
    }
    field.textContent = out.join('\n');
  }

  var resizeTimer;
  var lastWidth = 0;
  window.addEventListener('resize', function () {
    if (!field || field.clientWidth === lastWidth) return;
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () { lastWidth = field.clientWidth; drawField(); }, 150);
  });
  if (field) { lastWidth = field.clientWidth; drawField(); }
})();
