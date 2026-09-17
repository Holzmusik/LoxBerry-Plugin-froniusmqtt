#!/usr/bin/perl
# froniusmqtt Haupt-UI - eine einzige Single-Page-App. Optik/Struktur 1:1 vom
# Schwester-Plugin EaseeMQTT übernommen (Klasse .em-app -> .fm-app, sonst
# unverändert) - alle Plugins sollen gleich aussehen/sich gleich bedienen,
# siehe Plan-Datei. Inhaltlich an den Fronius-Bedarf angepasst: kein
# Charger-Picker (Geräte werden automatisch per GetActiveDeviceInfo erkannt,
# nicht manuell eingetragen), Daten-Seite mit 4 Kategorie-Schaltern statt
# 170 Einzel-Observations, zusätzliche Batterie-Control-Seite.
use LoxBerry::System;
use LoxBerry::Web;
use CGI;
use strict;
use warnings;

my $cgi = CGI->new;
print $cgi->header(-charset => 'utf-8');
LoxBerry::Web::lbheader("Fronius-MQTT Bridge");
print <<'HTML';
<style>
.fm-app {
  --bg:      var(--lb-bg, #f7f7f7);
  --surface: var(--lb-card-bg, #fff);
  --surf2:   var(--lb-input-bg, #f5f5f5);
  --brd:     var(--lb-border-color, #e5e5e5);
  --txt:     var(--lb-text, #171717);
  --txt2:    var(--lb-text-muted, #737373);
  --acc:     var(--lb-primary, #6dac20);
  --warn:    var(--lb-warning, #ca8a04);
  --danger:  var(--lb-danger, #dc2626);
  --radius:  var(--lb-radius, 12px);
  color: var(--txt); font: 14px/1.5 system-ui, sans-serif;
}
.fm-app, .fm-app * { box-sizing: border-box; margin: 0; padding: 0; }

.fm-app .shell { display: flex; min-height: 640px; background: var(--bg); border-radius: var(--radius); overflow: hidden; border: 1px solid var(--brd); }
.fm-app .sidebar { width: 220px; background: var(--surface); border-right: 1px solid var(--brd); display: flex; flex-direction: column; flex-shrink: 0; }
.fm-app .sidebar-logo { padding: 20px 18px 12px; font-size: 17px; font-weight: 700; color: var(--acc); letter-spacing: -.3px; border-bottom: 1px solid var(--brd); }
.fm-app .sidebar-logo span { color: var(--txt2); font-weight: 400; font-size: 12px; display: block; margin-top: 2px; }
.fm-app .nav { flex: 1; padding: 8px 0; }
.fm-app .nav-item { display: flex; align-items: center; gap: 10px; padding: 10px 18px; cursor: pointer; color: var(--txt2); border-left: 3px solid transparent; transition: all .15s; }
.fm-app .nav-item:hover { color: var(--txt); background: var(--surf2); }
.fm-app .nav-item.active { color: var(--acc); border-left-color: var(--acc); background: rgba(102,205,0,.07); }
.fm-app .main { flex: 1; overflow: hidden; padding: 28px; min-height: 0; display: flex; flex-direction: column; }

.fm-app .page { display: none; }
.fm-app .page.active { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow-y: auto; }
.fm-app h2 { font-size: 18px; font-weight: 600; margin-bottom: 20px; }
.fm-app h3 { font-size: 14px; font-weight: 600; margin-bottom: 14px; color: var(--txt2); text-transform: uppercase; letter-spacing: .5px; }
.fm-app .card { background: var(--surface); border: 1px solid var(--brd); border-radius: var(--radius); padding: 20px; margin-bottom: 16px; }
.fm-app .card.warn { border-color: var(--danger); background: rgba(220,38,38,.06); }
.fm-app .card-title { font-size: 15px; font-weight: 600; margin-bottom: 16px; }
.fm-app .card-title-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.fm-app .card-title-row .card-title { margin-bottom: 0; }
.fm-app .card-toggle { cursor: pointer; color: var(--txt2); font-size: 12px; user-select: none; }
.fm-app .card-toggle:hover { color: var(--txt); }
.fm-app .log-hidden { display: none; }
.fm-app .hint { font-size: 12px; color: var(--txt2); margin-top: 6px; }
.fm-app .hint.warn { color: var(--danger); font-weight: 500; }

.fm-app select {
  background-color: var(--surf2) !important;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%23888' d='M6 8L1 3h10z'/%3E%3C/svg%3E");
  background-repeat: no-repeat; background-position: right 12px center;
  appearance: none; -webkit-appearance: none; -moz-appearance: none;
  border: 1px solid var(--brd) !important; border-radius: 8px; padding: 9px 32px 9px 12px;
  color: var(--txt) !important; font-size: 14px; outline: none; cursor: pointer;
}
.fm-app select:focus { border-color: var(--acc) !important; box-shadow: 0 0 0 3px rgba(102,205,0,.15); }
.fm-app select option { background: var(--surf2) !important; color: var(--txt) !important; }
.fm-app input[type=text], .fm-app input[type=number], .fm-app input[type=password] {
  background: var(--surf2); border: 1px solid var(--brd); border-radius: 8px; padding: 9px 12px; color: var(--txt); font-size: 14px; outline: none;
}
.fm-app input:focus { border-color: var(--acc); }
.fm-app .toggle {
  width: 36px; height: 20px; min-width: 36px; flex-shrink: 0;
  border-radius: 10px; background: var(--surf2); border: 1px solid var(--brd);
  position: relative; cursor: pointer; transition: background .15s, border-color .15s;
}
.fm-app .toggle::after {
  content: ''; position: absolute; top: 1px; left: 1px;
  width: 16px; height: 16px; border-radius: 50%; background: var(--txt2);
  transition: transform .15s, background .15s;
}
.fm-app .toggle[data-checked="true"] { background: var(--acc); border-color: var(--acc); }
.fm-app .toggle[data-checked="true"]::after { transform: translateX(16px); background: #000; }
.fm-app .toggle.toggle-danger[data-checked="true"] { background: var(--danger); border-color: var(--danger); }
.fm-app .checkfield label { flex: 1; cursor: pointer; }

.fm-app .field { margin-bottom: 14px; }
.fm-app .field label { display: block; font-size: 12px; color: var(--txt2); margin-bottom: 5px; }
.fm-app .field input, .fm-app .field select { width: 100%; }
.fm-app .row { display: flex; gap: 12px; }
.fm-app .row .field { flex: 1; }
.fm-app .checkfield { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; }

.fm-app .btn { display: inline-flex; align-items: center; gap: 7px; padding: 9px 16px; border-radius: 8px; border: none; cursor: pointer; font-size: 14px; font-weight: 500; transition: opacity .15s; }
.fm-app .btn:hover { opacity: .85; }
.fm-app .btn-primary { background: var(--acc); color: #000; }
.fm-app .btn-secondary { background: var(--surf2); color: var(--txt); border: 1px solid var(--brd); }
.fm-app .btn-danger { background: var(--danger); color: #fff; }
.fm-app .btn-sm { padding: 6px 12px; font-size: 13px; }
.fm-app .btn-group { display: flex; gap: 10px; margin-top: 18px; margin-bottom: 20px; flex-wrap: wrap; }

.fm-app table.ga-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.fm-app table.ga-table th { text-align: left; color: var(--txt2); font-weight: 600; padding: 6px 8px; border-bottom: 1px solid var(--brd); font-size: 11px; text-transform: uppercase; letter-spacing: .3px; position: sticky; top: 0; background: #1c1c20 !important; z-index: 1; }
.fm-app table.ga-table td { padding: 5px 8px; border-bottom: 1px solid var(--brd); color: var(--txt) !important; }
.fm-app table.ga-table input, .fm-app table.ga-table select { width: 100%; padding: 6px 8px; font-size: 13px; }
.fm-app table.ga-table td.mono { font-variant-numeric: tabular-nums; color: var(--txt2) !important; white-space: nowrap; }
.fm-app .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }

.fm-app .badge { font-size: 11px; padding: 3px 8px; border-radius: 20px; background: var(--surf2); color: var(--txt2); border: 1px solid var(--brd); }
.fm-app .status-bar { display: flex; align-items: center; gap: 8px; padding: 10px 14px; background: var(--surf2); border-radius: 8px; margin-bottom: 16px; font-size: 13px; }
.fm-app .status-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--txt2); flex-shrink: 0; }
.fm-app .status-dot.ok { background: var(--acc); }
.fm-app .status-dot.err { background: var(--danger); }
.fm-app pre.log { background: #111; color: #0f0; padding: 12px; border-radius: 8px; overflow: auto; max-height: 45vh; font-size: 12px; }
.fm-app .toast { position: fixed; bottom: 24px; right: 24px; background: var(--surf2); border: 1px solid var(--brd); border-radius: 10px; padding: 12px 18px; font-size: 13px; box-shadow: 0 4px 20px rgba(0,0,0,.5); transform: translateY(80px); opacity: 0; transition: all .25s; z-index: 999; }
.fm-app .toast.show { transform: translateY(0); opacity: 1; }
.fm-app .toast.ok  { border-color: var(--acc); }
.fm-app .toast.err { border-color: var(--danger); }
.fm-app .divider { border: none; border-top: 1px solid var(--brd); margin: 20px 0; }

.fm-app .cat-group { background: var(--surf2); border: 1px solid var(--brd); border-radius: 10px; padding: 12px 14px 14px; margin-top: 10px; }
.fm-app .cat-row { display: flex; align-items: center; gap: 10px; padding: 8px 4px; }
.fm-app .cat-row + .cat-row { border-top: 1px solid var(--brd); }
.fm-app .cat-name { font-weight: 500; }
.fm-app .cat-desc { color: var(--txt2); font-size: 12px; flex: 1; }

@media (max-width: 680px) {
  .fm-app .shell { flex-direction: column; min-height: 0; overflow: visible; border-radius: 0; border: none; }
  .fm-app .sidebar { width: 100%; flex-direction: row; flex-shrink: 0; border-right: none; border-bottom: 1px solid var(--brd); }
  .fm-app .sidebar-logo { display: none; }
  .fm-app .nav { flex-direction: row; padding: 0; overflow-x: auto; }
  .fm-app .nav-item { flex-direction: column; gap: 3px; padding: 10px 14px; font-size: 11px; white-space: nowrap; border-left: none; border-bottom: 3px solid transparent; min-height: 52px; justify-content: center; align-items: center; }
  .fm-app .nav-item.active { border-left-color: transparent; border-bottom-color: var(--acc); }
  .fm-app .main { padding: 16px; overflow-y: visible; display: block; }
  .fm-app .row { flex-direction: column; gap: 0; }
  .fm-app .toast { left: 12px; right: 12px; bottom: 12px; }
}
</style>

<div class="fm-app" data-enhance="false">
<div class="shell">

<aside class="sidebar">
  <div class="sidebar-logo">FroniusMQTT<span>Fronius GEN24 &lt;-&gt; Loxone Brücke</span></div>
  <nav class="nav">
    <div class="nav-item active" data-page="overview">Übersicht</div>
    <div class="nav-item" data-page="mqtt">MQTT</div>
    <div class="nav-item" data-page="data">Daten</div>
    <div class="nav-item" data-page="battery">Batterie-Control</div>
    <div class="nav-item" data-page="loxone">Loxone-Import</div>
    <div class="nav-item" data-page="status">Status</div>
    <div class="nav-item" data-page="debug">Debug</div>
  </nav>
</aside>

<main class="main">

  <section id="page-overview" class="page active">
    <h2>Übersicht</h2>
    <div class="card">
      <div class="card-title">Dienst</div>
      <div class="status-bar"><span class="status-dot" id="dot-svc"></span><span id="txt-svc">froniusmqtt (Fronius &lt;-&gt; MQTT) ...</span></div>
      <div class="hint">Protokollbrücke: lokale Fronius Solar API v1 (unauthentifiziert, HTTP) -&gt; MQTT. Kein Zugriff über die Fronius-Cloud.</div>
    </div>
    <div class="card">
      <div class="card-title">Fronius-Wechselrichter</div>
      <div class="row">
        <div class="field"><label>Host / IP-Adresse</label><input type="text" id="fronius_host" data-role="none" placeholder="192.168.1.50"></div>
        <div class="field"><label>Poll-Intervall (Sekunden)</label><input type="number" id="poll_interval" data-role="none" placeholder="5" min="1"></div>
      </div>
      <div class="row">
        <div class="field" style="max-width:220px"><label>HTTP-Timeout (Sekunden)</label><input type="number" id="http_timeout" data-role="none" placeholder="5" min="1"></div>
      </div>
      <div class="btn-group">
        <button class="btn btn-primary" onclick="testFronius()">Verbindung testen</button>
        <button class="btn btn-secondary" onclick="saveConfig()">Speichern</button>
      </div>
      <span class="hint" id="fronius-test-status"></span>
      <div id="fronius-devices"></div>
      <div class="hint">Geräte werden automatisch erkannt (GetActiveDeviceInfo) - keine manuelle Eingabe von Geräte-IDs nötig. Zusätzlich angeschlossene Smartmeter/Speicher erscheinen nach einem erneuten Test bzw. automatisch im laufenden Betrieb.</div>
    </div>
  </section>

  <section id="page-mqtt" class="page">
    <h2>MQTT</h2>
    <div class="card">
      <div class="checkfield"><div class="toggle" id="use_local_broker" onclick="toggleSwitch('use_local_broker')"></div><label onclick="toggleSwitch('use_local_broker')">Lokalen LoxBerry-Broker verwenden</label></div>
      <div class="status-bar" id="local-broker-bar"><span class="status-dot ok"></span><span id="local-broker-info">MQTT-Broker (aus LoxBerry) ...</span></div>
      <div class="hint">Bei aktivem Schalter werden Host/Port/Zugangsdaten bei jedem Speichern automatisch aus LoxBerrys eigener MQTT-Gateway-Konfiguration übernommen.</div>
      <div class="divider"></div>
      <div id="external-broker-fields">
        <div class="row">
          <div class="field"><label>Broker-Host</label><input type="text" id="mqtt_host" data-role="none" placeholder="192.168.1.10"></div>
          <div class="field"><label>Port</label><input type="number" id="mqtt_port" data-role="none" placeholder="1883"></div>
        </div>
        <div class="row">
          <div class="field"><label>Benutzername</label><input type="text" id="mqtt_username" data-role="none"></div>
          <div class="field"><label>Passwort</label><input type="password" id="mqtt_password" data-role="none"></div>
        </div>
      </div>
      <div class="row">
        <div class="field"><label>Topic-Prefix</label><input type="text" id="topic_prefix" data-role="none" placeholder="fronius/"></div>
        <div class="field"><label>MQTT-Client-ID</label><input type="text" id="client_id" data-role="none" placeholder="froniusmqtt"></div>
      </div>
    </div>
    <div class="btn-group">
      <button class="btn btn-primary" onclick="saveConfig()">Speichern</button>
    </div>
  </section>

  <section id="page-data" class="page">
    <h2>Daten</h2>
    <div class="card">
      <div class="card-title">Welche Werte landen in MQTT?</div>
      <div class="hint" style="margin-bottom:10px">
        Fronius liefert deutlich weniger Werte als z.B. eine Easee-Wallbox -
        hier reicht eine Auswahl pro Kategorie statt einer langen
        Einzelwert-Liste.
      </div>
      <div class="cat-group" id="cat-groups"></div>
      <div class="btn-group">
        <button class="btn btn-primary" onclick="saveConfig()">Speichern</button>
      </div>
    </div>
  </section>

  <section id="page-battery" class="page">
    <h2>Batterie-Control</h2>
    <div class="card warn">
      <div class="card-title">⚠ Experimentell - undokumentierter Fronius-Endpunkt</div>
      <div class="hint warn">
        Dieses Feature schreibt über den NICHT offiziell dokumentierten
        Endpunkt <span class="mono">POST /config/timeofuse</span> (bzw.
        <span class="mono">/api/config/timeofuse</span> ab Firmware
        1.36.5-1) eine Entladesperre in die Wechselrichter-eigene
        Konfiguration "Energiemanagement -&gt; Batteriemanagement".
      </div>
      <div class="hint warn">
        Das ÜBERSCHREIBT dort ggf. bereits von dir eingerichtete Zeitpläne
        und kann mit einem Fronius-Firmware-Update ohne Vorwarnung brechen.
        Nur aktivieren, wenn du das willst und die Konsequenz kennst.
      </div>
    </div>
    <div class="card">
      <div class="checkfield"><div class="toggle toggle-danger" id="battery_enabled" onclick="toggleSwitch('battery_enabled')"></div><label onclick="toggleSwitch('battery_enabled')">Batterie-Control aktivieren</label></div>
      <div class="row">
        <div class="field">
          <label>Benutzername (Fronius-Weboberfläche)</label>
          <select id="battery_user" data-role="none">
            <option value="customer">customer</option>
            <option value="technician">technician</option>
          </select>
        </div>
        <div class="field"><label>Passwort</label><input type="password" id="battery_password" data-role="none"></div>
      </div>
      <div class="field" style="max-width:320px">
        <label>Endpunkt-Pfad</label>
        <select id="battery_config_path" data-role="none">
          <option value="auto">Automatisch erkennen</option>
          <option value="/config">/config/timeofuse (Firmware &lt; 1.36.5-1)</option>
          <option value="/api/config">/api/config/timeofuse (Firmware &gt;= 1.36.5-1)</option>
        </select>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary" onclick="testBattery()">Login testen (nur lesend)</button>
        <button class="btn btn-primary" onclick="saveConfig()">Speichern</button>
      </div>
      <span class="hint" id="battery-test-status"></span>
      <div class="hint">Der Testbutton führt NUR einen lesenden Aufruf aus, um die Zugangsdaten zu prüfen - es wird dabei nichts geschrieben/gesperrt. Die eigentliche Sperre/Freigabe löst ausschließlich ein MQTT-Kommando aus (siehe Loxone-Import-Seite), nie diese Web-UI.</div>
    </div>
  </section>

  <section id="page-loxone" class="page">
    <h2>Loxone-Import</h2>
    <div class="card">
      <div class="card-title">Wie kommen die Topics nach Loxone?</div>
      <div class="hint">
        Diese Bridge verbindet sich mit MQTT - Loxone selbst braucht dafür
        LoxBerrys eigenes <strong>"MQTT Gateway"</strong> als Vermittler. Dort müssen die
        Topics einmalig im Tab <strong>Abonnements</strong> abonniert werden (Wildcard auf Topic Präfix, oder jedes Topic separat).
        Sind die Topics abonniert, generiert diese Seite die fertigen Werte zum
        Copy-Paste für Loxone Config (Quelle: <a href="https://wiki.loxberry.de/konfiguration/widget_help/widget_mqtt/mqtt_gateway/mqtt_schritt_fur_schritt_mqtt_loxone" target="_blank" rel="noopener">LoxBerry-Wiki</a>).
      </div>
      <div class="hint">
        Welche Kategorien (Wechselrichter/Speicher/Zähler/Anlage) tatsächlich
        publiziert werden, legst du auf der <strong>"Daten"</strong>-Seite fest.
        <code>&lt;prefix&gt;bridge/status</code> (online/offline) ist der
        Last-Will-Status des Dienstes selbst und erscheint deshalb nicht in
        der Tabelle.
      </div>
      <div class="hint" id="loxone-devices-hint"></div>
      <div class="divider"></div>
      <div class="row">
        <div class="field" style="max-width:300px">
          <label>Protokoll im MQTT-Gateway (Tab "Gateway")</label>
          <select id="loxone-protocol" data-role="none" onchange="renderLoxoneExport()">
            <option value="udp">UDP (ein gemeinsamer Eingang für alle Werte, keine Einzelberechtigungen nötig)</option>
            <option value="http">HTTP (ein eigener Virtueller Eingang pro Wert, jeweils mit eigenen Berechtigungen)</option>
          </select>
        </div>
        <div class="field" style="max-width:260px">
          <label>Gerät</label>
          <select id="loxone-device" data-role="none" onchange="renderLoxoneExport()">
            <option value="">Alle Geräte</option>
          </select>
        </div>
        <div class="field" style="max-width:170px">
          <label>&nbsp;</label>
          <button class="btn btn-secondary" style="width:100%" onclick="testFronius(true)">Geräte neu laden</button>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Status-Topics -&gt; Virtuelle Eingänge</div>
      <div class="hint" style="margin-bottom:10px">Pro Zeile ein Loxone-Objekt anlegen. Klick in ein Feld wählt dessen Inhalt komplett aus (Strg+C) - Bezeichnung und Befehl sind bewusst getrennte Felder, damit du nur genau das kopierst, was gerade gebraucht wird.</div>
      <div style="overflow-x:auto; max-height:420px; overflow-y:auto">
        <table class="ga-table" id="loxone-state-table">
          <thead><tr id="loxone-state-thead"></tr></thead>
          <tbody id="loxone-state-tbody"></tbody>
        </table>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Kommandos -&gt; Virtueller Ausgang + Befehle</div>
      <div class="hint" style="margin-bottom:10px">
        Immer per UDP (unabhängig vom Protokoll oben - das betrifft nur die
        Status-Richtung). Ein einziger Virtueller Ausgang mit Adresse
        <span class="mono">/dev/udp/&lt;loxberry&gt;/&lt;Gateway-UDP-In-Port, Standard 11884&gt;</span>
        reicht, darunter je Zeile ein Virtueller Ausgang Befehl. Nur sichtbar,
        wenn Batterie-Control aktiviert ist.
      </div>
      <div style="overflow-x:auto">
        <table class="ga-table">
          <thead><tr><th>Bezeichnung (Mouseover: Wirkung)</th><th>Befehl bei EIN</th></tr></thead>
          <tbody id="loxone-cmd-tbody"></tbody>
        </table>
      </div>
    </div>
  </section>

  <section id="page-status" class="page">
    <h2>Status</h2>
    <div class="card">
      <div class="card-title-row">
        <div class="card-title">froniusmqtt</div>
        <span class="card-toggle" id="toggle-svc" onclick="toggleLog('svc')">Log anzeigen &#9662;</span>
      </div>
      <div class="status-bar"><span class="status-dot" id="dot-svc2"></span><span id="txt-svc2">...</span></div>
      <pre class="log log-hidden" id="log-svc"></pre>
    </div>
    <div class="btn-group">
      <button class="btn btn-secondary" onclick="restartService()">Dienst neu starten</button>
    </div>
  </section>

  <section id="page-debug" class="page">
    <h2>Debug</h2>
    <div class="card">
      <div class="hint">UI-Version: <span id="ui-build"></span> - hilft zu prüfen, ob nach einer
        Neuinstallation tatsächlich der neue Stand geladen wurde (Browser-Cache!).</div>
    </div>
    <div class="card">
      <div class="card-title">Web-UI-Anfragen (Browser, dieser Tab)</div>
      <pre class="log" id="log-client"></pre>
    </div>
    <div class="card">
      <div class="card-title">api.cgi (Server)</div>
      <pre class="log" id="log-api"></pre>
    </div>
  </section>

</main>
</div>
<div class="toast" id="toast"></div>
</div>

<script>
const API = 'api.cgi';

const UI_BUILD = '2026-09-17-01';
console.log('[FroniusMQTT] index.cgi UI_BUILD=' + UI_BUILD);

const CLIENT_LOG = [];
function clientLog(line) {
  const ts = new Date().toLocaleTimeString('de-DE');
  CLIENT_LOG.push('[' + ts + '] ' + line);
  if (CLIENT_LOG.length > 200) CLIENT_LOG.shift();
  const el = document.getElementById('log-client');
  if (el) el.textContent = CLIENT_LOG.join('\n');
  console.log('[FroniusMQTT] ' + line);
}

function redactForLog(data) {
  if (Array.isArray(data)) return data.map(redactForLog);
  if (data && typeof data === 'object') {
    const out = {};
    for (const k of Object.keys(data)) {
      out[k] = /pass|secret|token/i.test(k) ? '***' : redactForLog(data[k]);
    }
    return out;
  }
  return data;
}

async function api(action, opts) {
  opts = opts || {};
  const url = API + '?action=' + encodeURIComponent(action);
  const init = { method: opts.method || 'GET' };
  if (opts.body) { init.body = JSON.stringify(opts.body); init.headers = {'Content-Type':'application/json'}; }
  clientLog('-> ' + (init.method) + ' action=' + action);
  let res, text;
  try {
    res = await fetch(url, init);
    text = await res.text();
  } catch (e) {
    clientLog('<- ' + action + ': NETZWERKFEHLER ' + e.message);
    return { error: 'Netzwerkfehler: ' + e.message };
  }
  try {
    const data = JSON.parse(text);
    clientLog('<- ' + action + ': HTTP ' + res.status + ' ' + JSON.stringify(redactForLog(data)).slice(0, 200));
    return data;
  } catch (e) {
    clientLog('<- ' + action + ': HTTP ' + res.status + ', KEIN JSON: ' + text.slice(0, 150));
    return { error: 'Unerwartete Antwort vom Server (HTTP ' + res.status + ') - siehe Browser-Konsole (F12)' };
  }
}

function toast(msg, ok) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast show ' + (ok ? 'ok' : 'err');
  setTimeout(() => t.className = 'toast', 2500);
}

document.querySelectorAll('.nav-item').forEach(item => {
  item.addEventListener('click', () => {
    document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    item.classList.add('active');
    document.getElementById('page-' + item.dataset.page).classList.add('active');
    if (item.dataset.page === 'loxone') renderLoxoneExport();
  });
});

function toggleSwitch(id) { setToggled(id, !isToggled(id)); applyLocalBrokerVisibility(); }
function isToggled(id) { const el = document.getElementById(id); return el && el.dataset.checked === 'true'; }
function setToggled(id, val) { const el = document.getElementById(id); if (el) el.dataset.checked = val ? 'true' : 'false'; }

function applyLocalBrokerVisibility() {
  const local = isToggled('use_local_broker');
  document.getElementById('local-broker-bar').style.display = local ? '' : 'none';
  document.getElementById('external-broker-fields').style.display = local ? 'none' : '';
}

// -- Kategorien (Daten-Seite) --------------------------------------------
const CATEGORY_CATALOG = [
  { id: 'inverter', name: 'Wechselrichter', desc: 'Leistung, Ströme/Spannungen je Phase, Statuscodes, Tages-/Jahres-/Gesamtenergie, SOC (aus PowerFlow)' },
  { id: 'storage',  name: 'Speicher',       desc: 'Ladezustand, Kapazität, Lade-/Entladestrom, Temperatur, Batteriezustand (Reserva)' },
  { id: 'meter',    name: 'Zähler',         desc: 'Wirk-/Scheinleistung, Ströme/Spannungen je Phase, Energiezähler, Cos-Phi - je Smartmeter' },
  { id: 'site',     name: 'Anlage',         desc: 'Gesamtübersicht: Netz-/Haus-/PV-/Batterieleistung, Autarkie- und Eigenverbrauchsgrad' },
];
let SELECTED_CATEGORIES = new Set(CATEGORY_CATALOG.map(c => c.id));

function toggleCategory(id) {
  if (SELECTED_CATEGORIES.has(id)) SELECTED_CATEGORIES.delete(id); else SELECTED_CATEGORIES.add(id);
  renderCategoryList();
}

function renderCategoryList() {
  document.getElementById('cat-groups').innerHTML = CATEGORY_CATALOG.map(c =>
    '<div class="cat-row">' +
      '<div class="toggle" data-checked="' + SELECTED_CATEGORIES.has(c.id) + '" onclick="toggleCategory(\'' + c.id + '\')"></div>' +
      '<div class="cat-name" style="min-width:110px">' + c.name + '</div>' +
      '<div class="cat-desc">' + c.desc + '</div>' +
    '</div>'
  ).join('');
}

// -- Fronius-Verbindungstest / Geräte-Discovery ---------------------------
let LAST_DISCOVERY = null;

function deviceTypeLabel(kind) {
  return { inverters: 'Wechselrichter', meters: 'Zähler', storages: 'Speicher' }[kind] || kind;
}

function renderDevices(devices) {
  const el = document.getElementById('fronius-devices');
  if (!devices) { el.innerHTML = ''; return; }
  const rows = [];
  ['inverters', 'meters', 'storages'].forEach(kind => {
    (devices[kind] || []).forEach(d => {
      rows.push('<tr><td>' + deviceTypeLabel(kind) + '</td><td>' + d.id + '</td><td>' + (d.Serial || '') + '</td></tr>');
    });
  });
  if (rows.length === 0) { el.innerHTML = '<div class="hint">keine Geräte gefunden</div>'; return; }
  el.innerHTML = '<div style="overflow-x:auto;margin-top:10px"><table class="ga-table">' +
    '<thead><tr><th>Typ</th><th>ID</th><th>Seriennummer</th></tr></thead>' +
    '<tbody>' + rows.join('') + '</tbody></table></div>';
}

async function testFronius(silent) {
  const host = document.getElementById('fronius_host').value;
  const r = await api('fronius_test', { method: 'POST', body: { host } });
  const el = document.getElementById('fronius-test-status');
  if (!r.ok) {
    if (el) el.textContent = r.error || 'Verbindung fehlgeschlagen';
    if (!silent) toast(r.error || 'Verbindung fehlgeschlagen', false);
    return;
  }
  LAST_DISCOVERY = r.devices;
  if (el) el.textContent = 'OK - API-Version ' + (r.api_version && r.api_version.APIVersion) + ', CompatibilityRange ' + (r.api_version && r.api_version.CompatibilityRange);
  renderDevices(r.devices);
  if (!silent) toast('Verbindung OK', true);
  renderLoxoneExport();
}

async function testBattery() {
  const body = {
    host: document.getElementById('fronius_host').value,
    user: document.getElementById('battery_user').value,
    password: document.getElementById('battery_password').value,
    config_path: document.getElementById('battery_config_path').value,
  };
  const r = await api('battery_test', { method: 'POST', body });
  const el = document.getElementById('battery-test-status');
  el.textContent = r.ok ? ('Login OK (Pfad ' + r.path + ', HTTP ' + r.http_code + ')') : (r.error || 'Login fehlgeschlagen');
  toast(r.ok ? 'Batterie-Control-Login OK' : (r.error || 'Login fehlgeschlagen'), !!r.ok);
}

// -- Konfiguration laden/speichern -------------------------------
function currentConfigForm() {
  return {
    fronius: {
      host: document.getElementById('fronius_host').value,
      poll_interval_seconds: parseInt(document.getElementById('poll_interval').value || '5', 10),
      http_timeout_seconds: parseInt(document.getElementById('http_timeout').value || '5', 10),
      battery_control: {
        enabled: isToggled('battery_enabled'),
        user: document.getElementById('battery_user').value,
        password: document.getElementById('battery_password').value,
        config_path: document.getElementById('battery_config_path').value,
      },
    },
    mqtt: {
      use_local_broker: isToggled('use_local_broker'),
      host: document.getElementById('mqtt_host').value,
      port: parseInt(document.getElementById('mqtt_port').value || '1883', 10),
      username: document.getElementById('mqtt_username').value,
      password: document.getElementById('mqtt_password').value,
      topic_prefix: document.getElementById('topic_prefix').value || 'fronius/',
      client_id: document.getElementById('client_id').value || 'froniusmqtt',
      enabled_categories: Array.from(SELECTED_CATEGORIES),
    },
  };
}

function fillConfigForm(c) {
  const f = c.fronius || {};
  document.getElementById('fronius_host').value = f.host || '';
  document.getElementById('poll_interval').value = f.poll_interval_seconds || 5;
  document.getElementById('http_timeout').value = f.http_timeout_seconds || 5;

  const bc = f.battery_control || {};
  setToggled('battery_enabled', !!bc.enabled);
  document.getElementById('battery_user').value = bc.user || 'customer';
  document.getElementById('battery_password').value = '';
  document.getElementById('battery_password').placeholder = bc.password_set ? 'gespeichert - leer lassen zum Beibehalten' : '';
  document.getElementById('battery_config_path').value = bc.config_path || 'auto';

  const m = c.mqtt || {};
  setToggled('use_local_broker', m.use_local_broker !== false);
  document.getElementById('mqtt_host').value = m.host || '';
  document.getElementById('mqtt_port').value = m.port || 1883;
  document.getElementById('mqtt_username').value = m.username || '';
  document.getElementById('mqtt_password').value = '';
  document.getElementById('mqtt_password').placeholder = m.password_set ? 'gespeichert - leer lassen zum Beibehalten' : '';
  document.getElementById('topic_prefix').value = m.topic_prefix || 'fronius/';
  document.getElementById('client_id').value = m.client_id || 'froniusmqtt';
  document.getElementById('local-broker-info').textContent = 'MQTT-Broker (aus LoxBerry): ' + (c.local_broker_info || 'nicht konfiguriert');
  applyLocalBrokerVisibility();

  SELECTED_CATEGORIES = new Set(Array.isArray(m.enabled_categories) ? m.enabled_categories : CATEGORY_CATALOG.map(cc => cc.id));
  renderCategoryList();

  renderLoxoneExport();
}

// -- Loxone-Import: fertige Copy-Paste-Strings für LoxBerrys MQTT-Gateway --
// Muss 1:1 zu den Topic-Suffixen aus daemon/internal/fronius/poller.go
// passen - siehe dort für die Herkunft jedes Feldnamens.
const STATE_CATALOG = {
  inverter: [
    ['day_energy','Tagesertrag (Wh)'], ['year_energy','Jahresertrag (Wh)'], ['total_energy','Gesamtertrag (Wh)'],
    ['pac','Wirkleistung AC (W)'], ['sac','Scheinleistung AC (VA)'], ['iac','Strom AC (A)'], ['uac','Spannung AC (V)'],
    ['fac','Netzfrequenz (Hz)'], ['idc','Strom DC (A)'], ['udc','Spannung DC (V)'],
    ['error_code','Fehlercode'], ['status_code','Statuscode'], ['inverter_state','Betriebszustand (Text)'],
    ['uac_l1','Spannung L1 (V)'], ['uac_l2','Spannung L2 (V)'], ['uac_l3','Spannung L3 (V)'],
    ['iac_l1','Strom L1 (A)'], ['iac_l2','Strom L2 (A)'], ['iac_l3','Strom L3 (A)'],
    ['soc','Speicher-Ladezustand (%, aus PowerFlow)'], ['battery_mode','Batterie-Modus (Text)'],
  ],
  storage: [
    ['soc','Ladezustand (%)'], ['capacity_max','Kapazität max. (Wh)'], ['designed_capacity','Nennkapazität (Wh)'],
    ['current_dc','Lade-/Entladestrom DC (A)'], ['voltage_dc','Spannung DC (V)'], ['temperature','Zelltemperatur (°C)'],
    ['status_battery_cell','Batteriezustand-Code'], ['enable','Aktiv (0/1)'],
    ['manufacturer','Hersteller (Text)'], ['model','Modell (Text)'],
  ],
  meter: [
    ['power_sum','Wirkleistung Summe (W)'], ['power_l1','Wirkleistung L1 (W)'], ['power_l2','Wirkleistung L2 (W)'], ['power_l3','Wirkleistung L3 (W)'],
    ['power_apparent_sum','Scheinleistung Summe (VA)'], ['power_factor_sum','Leistungsfaktor Summe'],
    ['voltage_l1','Spannung L1 (V)'], ['voltage_l2','Spannung L2 (V)'], ['voltage_l3','Spannung L3 (V)'],
    ['current_l1','Strom L1 (A)'], ['current_l2','Strom L2 (A)'], ['current_l3','Strom L3 (A)'], ['current_sum','Strom Summe (A)'],
    ['energy_consumed','Energie Bezug (Wh)'], ['energy_produced','Energie Einspeisung (Wh)'],
    ['frequency','Netzfrequenz (Hz)'], ['meter_location','Meter-Position-Code'],
    ['manufacturer','Hersteller (Text)'], ['model','Modell (Text)'],
  ],
  site: [
    ['p_grid','Netzleistung (W, + = Bezug)'], ['p_load','Hausverbrauch (W)'], ['p_akku','Batterieleistung (W)'], ['p_pv','PV-Leistung (W)'],
    ['rel_autonomy','Autarkiegrad (%)'], ['rel_selfconsumption','Eigenverbrauchsgrad (%)'],
    ['battery_standby','Batterie im Standby (true/false)'], ['backup_mode','Ersatzstrom-Modus aktiv (true/false)'], ['mode','Anlagen-Modus (Text)'],
  ],
};

const COMMAND_CATALOG = [
  { topic: 'battery/cmd/hold', label: 'Batterie-Entladung sperren', desc: 'Aktiviert die Entladesperre über den experimentellen Endpunkt (siehe Batterie-Control-Seite)' },
  { topic: 'battery/cmd/release', label: 'Batterie-Entladung freigeben', desc: 'Hebt die Entladesperre wieder auf (Normalbetrieb)' },
];

function httpName(topic) {
  return topic.replace(/[\/ %]/g, '_');
}

function roCell(value, title) {
  const v = (value || '').replace(/"/g, '&quot;');
  const t = title ? ' title="' + title.replace(/"/g, '&quot;') + '"' : '';
  return '<td><input type="text" class="mono" readonly data-role="none" value="' + v + '"' + t + ' onclick="this.select()"></td>';
}

// Baut die Geräte-Auswahl (Dropdown neben dem Protokoll-Schalter) aus dem
// letzten Discovery-Ergebnis auf - eigene Funktion statt inline, damit die
// aktuelle Auswahl beim Neu-Rendern erhalten bleibt, solange sie noch gültig
// ist (gleiches Muster wie EaseeMQTTs refreshLoxoneChargerOptions()).
function loxoneDeviceOptions() {
  const opts = [{ value: '', label: 'Alle Geräte' }];
  if (LAST_DISCOVERY) {
    (LAST_DISCOVERY.inverters || []).forEach(d => opts.push({ value: 'inverter:' + d.id, label: 'Wechselrichter ' + d.id }));
    (LAST_DISCOVERY.storages || []).forEach(d => opts.push({ value: 'storage:' + d.id, label: 'Speicher ' + d.id }));
    (LAST_DISCOVERY.meters || []).forEach(d => opts.push({ value: 'meter:' + d.id, label: 'Zähler ' + d.id }));
  }
  opts.push({ value: 'site:', label: 'Anlage (gesamt)' });
  return opts;
}

function refreshLoxoneDeviceOptions() {
  const sel = document.getElementById('loxone-device');
  const current = sel.value;
  const opts = loxoneDeviceOptions();
  sel.innerHTML = opts.map(o => '<option value="' + o.value + '">' + o.label + '</option>').join('');
  if (opts.some(o => o.value === current)) sel.value = current;
}

function renderLoxoneExport() {
  const protocol = document.getElementById('loxone-protocol').value;
  const prefix = document.getElementById('topic_prefix').value || 'fronius/';
  const hintEl = document.getElementById('loxone-devices-hint');
  const theadEl = document.getElementById('loxone-state-thead');
  const stateBodyEl = document.getElementById('loxone-state-tbody');
  const cmdBodyEl = document.getElementById('loxone-cmd-tbody');

  refreshLoxoneDeviceOptions();

  if (!LAST_DISCOVERY) {
    hintEl.textContent = 'Noch keine Geräte bekannt - bitte zuerst auf der Übersicht-Seite "Verbindung testen" klicken.';
    theadEl.innerHTML = ''; stateBodyEl.innerHTML = ''; cmdBodyEl.innerHTML = '';
    return;
  }
  hintEl.textContent = '';

  // "" = alle Geräte (bisheriges Verhalten). Sonst "<kategorie>:<id>"
  // (z.B. "inverter:1", "site:" ohne ID) - schränkt die Tabelle auf genau
  // dieses eine Gerät ein.
  const deviceSel = document.getElementById('loxone-device').value;
  let filterCategory = null, filterId = null;
  if (deviceSel) {
    const parts = deviceSel.split(':');
    filterCategory = parts[0];
    filterId = parts[1] || null;
  }

  const rows = [];
  const addRows = (category, ids) => {
    if (!SELECTED_CATEGORIES.has(category)) return;
    if (filterCategory && filterCategory !== category) return;
    const useIds = (filterCategory === category && filterId) ? ids.filter(id => String(id) === filterId) : ids;
    (STATE_CATALOG[category] || []).forEach(([suffix, desc]) => {
      useIds.forEach(id => {
        const topic = prefix + category + (id !== null ? '/' + id : '') + '/state/' + suffix;
        const label = deviceTypeLabel(category === 'inverter' ? 'inverters' : category === 'meter' ? 'meters' : category === 'storage' ? 'storages' : category) +
          (id !== null ? ' ' + id : '') + ' - ' + desc;
        rows.push({ label, topic, desc });
      });
    });
  };
  addRows('inverter', (LAST_DISCOVERY.inverters || []).map(d => d.id));
  addRows('storage', (LAST_DISCOVERY.storages || []).map(d => d.id));
  addRows('meter', (LAST_DISCOVERY.meters || []).map(d => d.id));
  addRows('site', [null]);

  if (protocol === 'udp') {
    theadEl.innerHTML = '<th>Bezeichnung</th><th>Befehlserkennung</th>';
    stateBodyEl.innerHTML = rows.map(r =>
      '<tr>' + roCell(r.label, r.desc) + roCell('MQTT:\\i' + r.topic + '=\\i\\v') + '</tr>'
    ).join('');
  } else {
    theadEl.innerHTML = '<th>Bezeichnung (= Virtueller Eingang Name)</th>';
    stateBodyEl.innerHTML = rows.map(r =>
      '<tr>' + roCell(httpName(r.topic), r.desc) + '</tr>'
    ).join('');
  }

  const batteryEnabled = isToggled('battery_enabled');
  cmdBodyEl.innerHTML = !batteryEnabled ? '' : COMMAND_CATALOG.map(cmd => {
    const topic = prefix + cmd.topic;
    return '<tr>' + roCell(cmd.label, cmd.desc) + roCell('publish ' + topic + ' 1') + '</tr>';
  }).join('');
}

async function loadConfig() {
  const c = await api('config');
  fillConfigForm(c);
}

async function saveConfig() {
  const body = currentConfigForm();
  const r = await api('config', { method: 'POST', body });
  toast(r.ok ? 'Gespeichert - Dienst wird neu gestartet ...' : (r.error || 'Fehler'), !!r.ok);
}

// -- Status ----------------------------------------------------
function toggleLog(name) {
  const pre = document.getElementById('log-' + name);
  const btn = document.getElementById('toggle-' + name);
  if (!pre || !btn) return;
  const show = pre.classList.toggle('log-hidden') === false;
  btn.textContent = show ? 'Log verbergen ^' : 'Log anzeigen v';
}

function applyServiceStatus(s) {
  const ok = !!s.running;
  document.querySelectorAll('#dot-svc, #dot-svc2').forEach(el => el.className = 'status-dot ' + (ok ? 'ok' : 'err'));
  const txt = (ok ? 'aktiv' : 'inaktiv') + (s.pid ? ' (PID ' + s.pid + ')' : '');
  document.getElementById('txt-svc').textContent = 'froniusmqtt: ' + txt;
  document.getElementById('txt-svc2').textContent = txt;
  const log = document.getElementById('log-svc');
  if (log && s.log !== undefined) log.textContent = s.log;
}

async function restartService() {
  const r = await api('service_restart', { method: 'POST' });
  toast(r.ok ? 'Dienst neu gestartet' : 'Fehler beim Neustart', !!r.ok);
}

async function pollStatus() {
  const s = await api('service_status');
  if (!s.error) applyServiceStatus(s);
  const l = await api('api_log');
  const el = document.getElementById('log-api');
  if (el && l.log !== undefined) el.textContent = l.log;
}

async function step(name, fn) {
  try { await fn(); }
  catch (e) {
    console.error('init-Schritt "' + name + '" fehlgeschlagen:', e);
    toast('Fehler beim Laden (' + name + '): ' + e.message, false);
  }
}

(async function init() {
  document.getElementById('ui-build').textContent = UI_BUILD;
  renderCategoryList();
  await step('config', loadConfig);
  await step('fronius-test', () => testFronius(true));
  await step('status', pollStatus);
  setInterval(pollStatus, 5000);
})();
</script>
HTML

LoxBerry::Web::lbfooter();
