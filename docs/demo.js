// A replayed asm session: update, install, list. The text, the spinner, the
// progress bar and the narrow-terminal layouts follow the CLI's own output
// code, so what the page shows is what the tool prints.
(function () {
  'use strict';

  var screen = document.getElementById('term-screen');
  if (!screen) return;
  var shell = document.getElementById('term-shell');
  var note = document.getElementById('term-speed');
  var replay = document.getElementById('term-replay');
  var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  var ROWS = 23;
  var SPEED = 6; // long steps run this many times faster than real time
  var RATE = 21.5e6; // bytes per second the download pretends to reach

  // Archive sizes are the 51.4.1.1 compiler builds from the shockpkg catalog.
  var hosts = {
    windows: { shell: 'PowerShell', prompt: 'PS C:\\> ', root: 'C:\\AIRSDK\\', size: 598524117 },
    macos: { shell: 'zsh', prompt: '~ % ', root: '/Users/dev/AIRSDK/', size: 580152336 },
    linux: { shell: 'bash', prompt: '~ $ ', root: '/home/dev/AIRSDK/', size: 211661955 }
  };
  var os = hosts[window.asmOS] ? window.asmOS : 'windows';

  // ---- screen model --------------------------------------------------------

  // Each entry renders itself for a terminal width, the way the CLI asks
  // for the width on every draw.
  var entries = [];
  var cols = 80;

  function seg(text, cls) { return { t: text, c: cls || '' }; }
  function repeat(text, count) { return new Array(Math.max(0, count) + 1).join(text); }
  function padEnd(text, width) { return text + repeat(' ', width - text.length); }

  function wrapLines(text, indent, cls) {
    var width = Math.max(1, cols - indent - 1);
    var pad = repeat(' ', indent);
    var runes = Array.from(text);
    var out = [];
    while (runes.length > width) {
      var end = width;
      for (var i = width; i > 0; i--) {
        if (runes[i] === ' ') { end = i; break; }
      }
      out.push([seg(pad + runes.slice(0, end).join(''), cls)]);
      runes = Array.from(runes.slice(end).join('').replace(/^ +/, ''));
    }
    out.push([seg(pad + runes.join(''), cls)]);
    return out;
  }

  function add(render) {
    var entry = { render: render };
    entries.push(entry);
    return entry;
  }
  function drop(entry) {
    var index = entries.indexOf(entry);
    if (index >= 0) entries.splice(index, 1);
  }
  function text(value, cls) { add(function () { return [[seg(value, cls)]]; }); }
  function blank() { text(''); }
  function wrapped(value, indent, cls) { add(function () { return wrapLines(value, indent, cls); }); }
  function heading(value) { blank(); wrapped(value, 2, 'a'); blank(); }

  function table(headers, rows) {
    add(function () {
      var columns = headers.length - 1;
      var widths = [];
      var prefix = 2 + columns * 2;
      for (var i = 0; i < columns; i++) {
        widths[i] = headers[i].length;
        rows.forEach(function (row) { widths[i] = Math.max(widths[i], row[i].length); });
        prefix += widths[i];
      }
      var out = [];
      if (cols - prefix < 18) {
        rows.forEach(function (row) {
          out = out.concat(wrapLines(row.slice(0, columns).join(' -> '), 2, 'a'));
          out = out.concat(wrapLines(row[columns], 4, 'm'));
          out.push([seg('')]);
        });
        return out;
      }
      var header = '  ';
      for (var h = 0; h < columns; h++) header += padEnd(headers[h], widths[h]) + '  ';
      out.push([seg(header + headers[columns], 'm')]);
      rows.forEach(function (row) {
        var lead = [seg('  ')];
        for (var c = 0; c < columns; c++) lead.push(seg(padEnd(row[c], widths[c]), 'a'), seg('  '));
        var path = Array.from(row[columns]);
        var available = cols - prefix - 1;
        while (path.length > available) {
          out.push(lead.concat(seg(path.slice(0, available).join(''))));
          path = path.slice(available);
          lead = [seg(repeat(' ', prefix))];
        }
        out.push(lead.concat(seg(path.join(''))));
      });
      return out;
    });
  }

  function formatBytes(bytes) {
    var units = ['B', 'KB', 'MB', 'GB'];
    var unit = 0;
    while (bytes >= 1000 && unit < 3) { bytes /= 1000; unit++; }
    bytes = Math.round(bytes * 100) / 100;
    return (bytes === Math.trunc(bytes) ? bytes.toFixed(0) : bytes.toFixed(2)) + ' ' + units[unit];
  }

  // ---- drawing -------------------------------------------------------------

  function escape(value) {
    return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  // A line longer than the terminal continues on the next row, the way the
  // terminal itself wraps whatever the CLI prints with plain line().
  function softWrap(line) {
    var rows = [[]];
    var used = 0;
    line.forEach(function (part) {
      var runes = Array.from(part.t);
      while (runes.length) {
        if (used === cols) { rows.push([]); used = 0; }
        var take = runes.splice(0, cols - used);
        rows[rows.length - 1].push(seg(take.join(''), part.c));
        used += take.length;
      }
    });
    return rows;
  }

  function measure() {
    var probe = document.createElement('span');
    probe.textContent = repeat('M', 20);
    probe.style.visibility = 'hidden';
    probe.style.position = 'absolute';
    screen.appendChild(probe);
    var cell = probe.getBoundingClientRect().width / 20;
    screen.removeChild(probe);
    var style = getComputedStyle(screen);
    var inner = screen.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    cols = Math.max(20, Math.floor(inner / (cell || 7.8)));
  }

  var shown = '';
  function draw() {
    var lines = [];
    entries.forEach(function (entry) {
      entry.render().forEach(function (line) { lines = lines.concat(softWrap(line)); });
    });
    var html = lines.slice(-ROWS).map(function (line) {
      return line.map(function (part) {
        var body = escape(part.t);
        return part.c ? '<span class="' + part.c + '">' + body + '</span>' : body;
      }).join('');
    }).join('\n');
    if (html !== shown) {
      screen.innerHTML = html;
      shown = html;
    }
  }

  // ---- a clock that stops while nobody is watching -------------------------

  var clock = 0;
  var waiters = [];
  var visible = false;
  var instant = false;
  var generation = 0;
  var CANCELLED = {};

  function wait(ms) {
    if (instant) return Promise.resolve();
    return new Promise(function (resolve) { waiters.push({ at: clock + ms, resolve: resolve }); });
  }

  var last = performance.now();
  setInterval(function () {
    var now = performance.now();
    var step = Math.min(100, now - last);
    last = now;
    if (!visible || document.hidden) return;
    clock += step;
    waiters = waiters.filter(function (waiter) {
      if (waiter.at > clock) return true;
      waiter.resolve();
      return false;
    });
    draw();
  }, 50);

  // ---- the steps of a session ----------------------------------------------

  function checked(run) {
    return function (stamp) {
      if (stamp !== generation) throw CANCELLED;
      return run.apply(null, Array.prototype.slice.call(arguments, 1));
    };
  }

  var pause = checked(function (ms) { return wait(ms); });

  var type = checked(function (command) {
    var line = { typed: '', caret: true };
    add(function () {
      var parts = [seg(hosts[os].prompt, 'p'), seg(line.typed)];
      if (line.caret) parts.push(seg(' ', 'caret'));
      return [parts];
    });
    if (instant) {
      line.typed = command;
      line.caret = false;
      return Promise.resolve();
    }
    var stamp = generation;
    var chars = Array.from(command);
    var index = 0;
    function next() {
      if (stamp !== generation) return Promise.reject(CANCELLED);
      if (index === chars.length) {
        return wait(380).then(function () { line.caret = false; });
      }
      line.typed += chars[index];
      // An uneven rhythm reads as typing; a fixed one reads as a ticker.
      var delay = 55 + ((index * 37) % 50);
      index++;
      return wait(delay).then(next);
    }
    return wait(650).then(next);
  });

  var spinner = checked(function (label, ms, factor) {
    if (instant) return Promise.resolve();
    var started = clock;
    var entry = add(function () {
      var frame = '|/-\\'.charAt(Math.floor((clock - started) / 100) % 4);
      var seconds = Math.floor((clock - started) / 1000 * factor);
      var value = '  ' + frame + '  ' + label + '  ' + seconds + 's';
      return [[seg(Array.from(value).slice(0, Math.max(1, cols - 1)).join(''), 'a')]];
    });
    return wait(ms).then(function () { drop(entry); });
  });

  var download = checked(function (total) {
    if (instant) return Promise.resolve();
    var started = clock;
    var duration = total / RATE / SPEED * 1000;
    var entry = add(function () {
      var p = Math.min(1, (clock - started) / duration);
      // A little unevenness, kept monotonic, so the speed column moves.
      var shaped = Math.min(1, p + 0.03 * Math.sin(p * 9) * p * (1 - p));
      var received = Math.round(total * shaped);
      var seconds = (clock - started) / 1000 * SPEED;
      var width = Math.max(1, cols - 1);
      var stats = formatBytes(received) + ' / ' + formatBytes(total);
      if (seconds > 0) stats += '  ' + formatBytes(received / seconds) + '/s';
      var percent = ('   ' + Math.min(100, Math.floor(received * 100 / total))).slice(-3) + '%';
      var value;
      if (width >= 59) {
        var barWidth = Math.max(8, Math.min(28, width - stats.length - 13));
        var filled = Math.min(barWidth, Math.floor(received / total * barWidth));
        var bar = repeat('=', filled);
        if (filled < barWidth) { bar += '>'; filled++; }
        bar += repeat('.', barWidth - filled);
        value = '  [' + bar + '] ' + percent + '  ' + stats;
      } else {
        var frame = '|/-\\'.charAt(Math.floor((clock - started) / 100) % 4);
        value = '  ' + frame + '  ' + percent + '  ' + formatBytes(received);
      }
      return [[seg(Array.from(value).slice(0, width).join(''), 'a')]];
    });
    return wait(duration).then(function () { drop(entry); });
  });

  function session(stamp) {
    var host = hosts[os];
    var current = host.root + 'AIRSDK_51.3.4.1';
    var fresh = host.root + 'AIRSDK_51.4.1.1';
    entries = [];
    return pause(stamp, 0)
      .then(function () { return type(stamp, 'asm update'); })
      .then(function () { return spinner(stamp, 'Checking AIR SDK releases', 900, 1); })
      .then(function () {
        heading('Available updates');
        table(['Installed', 'Available', 'Path'], [['51.3.4.1', '51.3.4.3', current]]);
        blank();
        text('New AIR SDK available: 51.4.1.1', 'a');
        text('Install: asm install 51.4');
        blank();
        wrapped('Apply: asm update --all', 2, 'a');
        blank();
        return pause(stamp, 2200);
      })
      .then(function () { return type(stamp, 'asm install 51.4'); })
      .then(function () { return spinner(stamp, 'Checking AIR SDK releases', 800, 1); })
      .then(function () {
        heading('Install AIR SDK 51.4.1.1');
        text('Destination: ' + fresh, 'm');
        return spinner(stamp, 'Loading the SDK manifest', 600, 1);
      })
      .then(function () {
        text('Downloading AIR SDK 51.4.1.1', 'm');
        return download(stamp, host.size);
      })
      .then(function () { return spinner(stamp, 'Extracting the SDK', 1700, SPEED); })
      .then(function () { return spinner(stamp, 'Configuring the SDK', os === 'linux' ? 900 : 300, 1); })
      .then(function () {
        text('Installed AIR SDK 51.4.1.1', 'a');
        text('Path: ' + fresh, 'm');
        blank();
        return pause(stamp, 1600);
      })
      .then(function () { return type(stamp, 'asm list'); })
      .then(function () {
        heading('Installed AIR SDKs');
        table(['Version', 'Path'], [['51.4.1.1', fresh], ['51.3.4.1', current]]);
        blank();
        add(function () { return [[seg(hosts[os].prompt, 'p'), seg(' ', 'caret')]]; });
      });
  }

  // ---- playing -------------------------------------------------------------

  var state = 'waiting'; // waiting, playing, done

  function idleScreen() {
    entries = [];
    add(function () { return [[seg(hosts[os].prompt, 'p'), seg(' ', 'caret')]]; });
    draw();
  }

  function play(animate) {
    var stamp = ++generation;
    waiters = [];
    instant = !animate;
    state = 'playing';
    replay.hidden = true;
    note.textContent = animate ? 'sped-up replay' : 'recorded session';
    session(stamp).then(function () {
      instant = false;
      if (stamp !== generation) return;
      state = 'done';
      replay.hidden = false;
      draw();
    }, function (error) {
      if (error !== CANCELLED) throw error;
    });
    if (!animate) draw();
  }

  function setHost(name) {
    os = hosts[name] ? name : 'windows';
    shell.textContent = hosts[os].shell;
  }

  replay.addEventListener('click', function () { play(true); });

  document.addEventListener('asm:os', function (event) {
    setHost(event.detail);
    if (state === 'waiting') idleScreen();
    else if (state === 'playing' && !instant) play(true);
    else play(false);
  });

  var resizeTimer;
  window.addEventListener('resize', function () {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () { measure(); draw(); }, 100);
  });

  setHost(os);
  measure();
  idleScreen();

  function start() {
    if (state !== 'waiting') return;
    play(!reduced);
  }

  if ('IntersectionObserver' in window) {
    new IntersectionObserver(function (seen) {
      visible = seen[0].isIntersecting;
      if (visible) start();
    }, { threshold: 0.35 }).observe(screen);
  } else {
    visible = true;
    start();
  }
})();
