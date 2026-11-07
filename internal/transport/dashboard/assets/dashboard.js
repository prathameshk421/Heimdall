// Live updates for one change. The server sends one "change" event for each new log event. The page adds a
// row, and it updates the state, the gate and the version. The page never reads the log itself.
(function () {
  "use strict";

  var live = document.getElementById("live");
  var status = document.getElementById("live-status");
  if (!live || !window.EventSource) {
    if (status) {
      status.textContent = "Live updates are not available in this browser. Reload to see new events.";
    }
    return;
  }

  var timeline = document.getElementById("timeline");
  var state = document.getElementById("state");
  var gate = document.getElementById("gate");
  var version = document.getElementById("version");

  function cell(text) {
    var td = document.createElement("td");
    td.textContent = text;
    return td;
  }

  function addRow(item) {
    if (!timeline || timeline.querySelector('tr[data-version="' + item.version + '"]')) {
      return;
    }
    var row = document.createElement("tr");
    row.setAttribute("data-version", String(item.version));
    row.appendChild(cell(String(item.version)));
    row.appendChild(cell(item.at));
    row.appendChild(cell(item.type));
    row.appendChild(cell(item.actor));
    row.appendChild(cell(item.note));
    timeline.appendChild(row);
  }

  var url = live.getAttribute("data-stream") + "?after=" + encodeURIComponent(live.getAttribute("data-after"));
  var source = new EventSource(url);

  source.addEventListener("open", function () {
    status.textContent = "Live updates: connected";
  });

  source.addEventListener("error", function () {
    status.textContent = "Live updates: reconnecting";
  });

  source.addEventListener("change", function (ev) {
    var item;
    try {
      item = JSON.parse(ev.data);
    } catch (err) {
      status.textContent = "Live updates: one event could not be read. Reload the page.";
      return;
    }
    addRow(item);
    if (state) { state.textContent = item.state; }
    if (gate) { gate.textContent = item.gate; }
    if (version) { version.textContent = String(item.version); }
    live.setAttribute("data-after", String(item.version));
  });
})();
