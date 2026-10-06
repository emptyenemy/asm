// Switch between the Windows and the Unix install command.
document.querySelectorAll('.tab').forEach(function (tab) {
  tab.addEventListener('click', function () {
    document.querySelectorAll('.tab').forEach(function (other) {
      var on = other === tab;
      other.setAttribute('aria-selected', on ? 'true' : 'false');
      document.getElementById(other.getAttribute('aria-controls')).hidden = !on;
    });
  });
});

// Copy the install command. The clipboard API needs a secure context, so the
// fallback covers a local file:// review of this page.
document.querySelectorAll('.copy').forEach(function (button) {
  button.addEventListener('click', function () {
    var text = document.getElementById(button.dataset.target).textContent;
    var done = function () {
      button.textContent = 'COPIED';
      button.dataset.done = 'yes';
      setTimeout(function () {
        button.textContent = 'COPY';
        delete button.dataset.done;
      }, 1600);
    };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, done);
      return;
    }
    var area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    try { document.execCommand('copy'); } catch (error) { /* nothing to report here */ }
    document.body.removeChild(area);
    done();
  });
});
