# LoxBerry-Plugin-froniusmqtt

Reine Protokollbrücke zwischen einem Fronius GEN24-Wechselrichter (inkl.
Fronius Reserva-Speicher) und MQTT, für LoxBerry. Liest die lokale
**Fronius Solar API v1** (HTTP/JSON, unauthentifiziert im LAN) und
publiziert alle Werte als retained MQTT-Topics - für Fälle, in denen die
bestehende Modbus-TCP-Anbindung an Loxone nicht alle gewünschten Werte
liefert (z.B. detaillierter Batteriezustand, Statuscodes, Meter-Details).

Alle Endpunkte/Feldnamen sind **live gegen ein echtes Gerät verifiziert**
(Fronius GEN24 12.0 SC + Reserva, 2026-09-17)

## Architektur

```
Fronius GEN24 (lokale Solar API v1) <-> froniusmqtt (Go, systemd: froniusmqtt.service) <-> MQTT-Broker <-> Loxone
```

Der Go-Daemon (`daemon/`, aus dem mitgelieferten Quellcode gebaut, kein
externer `git clone` nötig):

1. Ermittelt per `GetActiveDeviceInfo.cgi` die Geräte (Discovery -
   Wechselrichter/Speicher/Zähler-IDs, **nicht hartcodiert**). Das Ergebnis
   wird 10 Minuten gecacht und nach einem Abruffehler sofort neu ermittelt.
   Neu angeschlossene Geräte erscheinen also spätestens nach 10 Minuten,
   ohne dass der Dienst neu starten muss. Unterstützt ein Wechselrichter
   `3PInverterData` nicht, wird das bis zur nächsten Discovery nicht erneut
   versucht.
2. Ruft für jedes gefundene Gerät die passenden Detail-Endpunkte ab
   (`GetInverterRealtimeData.cgi`, `GetStorageRealtimeData.cgi`,
   `GetMeterRealtimeData.cgi`, `GetPowerFlowRealtimeData.fcgi`)
3. Übersetzt jedes Feld 1:1 -> retained MQTT-State-Topic, publiziert aber
   **nur geänderte Werte**. Alle 5 Minuten und nach jedem MQTT-Reconnect
   gehen einmal alle Topics raus. Gerundet wird nach Größe: W/Wh ganzzahlig,
   V/%/°C auf 1 Stelle, A/Hz/cos φ auf 2 Stellen, damit Messrauschen
   keine Dauer-Änderungen erzeugt.
4. Optional (standardmäßig **deaktiviert**, siehe Warnung unten): abonniert
   `battery/cmd/hold` und `battery/cmd/release` -> schreibt eine
   Entladesperre über einen undokumentierten Fronius-Endpunkt
5. Optional (ebenfalls standardmäßig **deaktiviert**): liest und setzt den
   minimalen Ladezustand (`battery/state/soc_*`, `battery/cmd/soc_min`),
   siehe "Min-SoC-Steuerung" unten

Keine eigene Lastmanagement-/Automatisierungslogik - die bleibt vollständig
in Loxone.

## MQTT-Topic-Schema

Präfix konfigurierbar (Default `fronius/`). Kategorien (Wechselrichter/
Speicher/Zähler/Anlage) einzeln in der Web-UI ("Daten"-Seite) ab-/anwählbar.
Abgewählte Kategorien werden gar nicht erst vom Wechselrichter abgefragt
(PowerFlow läuft weiter, solange "Anlage" oder "Wechselrichter" aktiv ist,
weil er auch `inverter/<id>/state/soc|battery_mode` liefert).

```
<prefix>inverter/<id>/state/pac|sac|iac|uac|fac|idc|udc
<prefix>inverter/<id>/state/day_energy|year_energy|total_energy
<prefix>inverter/<id>/state/error_code|status_code|inverter_state
<prefix>inverter/<id>/state/uac_l1|uac_l2|uac_l3|iac_l1|iac_l2|iac_l3
<prefix>inverter/<id>/state/soc|battery_mode                    (aus PowerFlow)

<prefix>storage/<id>/state/soc|capacity_max|designed_capacity
<prefix>storage/<id>/state/current_dc|voltage_dc|temperature
<prefix>storage/<id>/state/status_battery_cell|enable|manufacturer|model

<prefix>meter/<id>/state/power_sum|power_l1|power_l2|power_l3
<prefix>meter/<id>/state/power_apparent_sum|power_factor_sum
<prefix>meter/<id>/state/voltage_l1|voltage_l2|voltage_l3
<prefix>meter/<id>/state/current_l1|current_l2|current_l3|current_sum
<prefix>meter/<id>/state/energy_consumed|energy_produced|frequency|meter_location

<prefix>site/state/p_grid|p_load|p_akku|p_pv
<prefix>site/state/rel_autonomy|rel_selfconsumption
<prefix>site/state/battery_standby|backup_mode|mode

<prefix>bridge/status                    (online/offline, Last-Will des Daemons selbst)
<prefix>bridge/fronius_ok                (1/0, retained, nach jedem Poll-Zyklus: Wechselrichter erreichbar)
<prefix>bridge/last_poll                 (Unix-Zeit des letzten erfolgreichen Zyklus, retained)

# nur wenn Batterie-Control aktiviert:
<prefix>battery/cmd/hold                 (Entladesperre aktivieren, Payload beliebig)
<prefix>battery/cmd/release              (Freigabe/Normalbetrieb, Payload beliebig)
<prefix>battery/state/control_mode       (normal|hold, retained - Rückmeldung)

# nur wenn Min-SoC-Steuerung aktiviert:
<prefix>battery/cmd/soc_min              (minimaler Ladezustand in %, Ganzzahl)
<prefix>battery/state/soc_min            (minimaler Ladezustand %, vom Gerät gelesen, retained)
<prefix>battery/state/soc_max            (maximaler Ladezustand %, vom Gerät gelesen, retained)
<prefix>battery/state/soc_mode           (manual|auto, retained)
<prefix>battery/state/last_write         (Unix-Zeit des letzten erfolgreichen Schreibens, retained)
```

`<id>` entspricht der von `GetActiveDeviceInfo.cgi` gemeldeten Geräte-ID
(beim Referenzgerät: 1 Wechselrichter, 2 Zähler, 1 Speicher). Werte, die
Fronius als JSON `null` liefert (z.B. `day_energy` direkt nach Mitternacht,
bevor der erste Tageswert existiert), werden bewusst **nicht** publiziert
statt fälschlich als `0` - retained MQTT behält ohnehin den letzten
bekannten Wert.

Ist der Wechselrichter nicht erreichbar (Timeout, Verbindung abgelehnt), bricht
der Zyklus beim ersten Fehler ab, statt jeden weiteren Endpunkt in den
HTTP-Timeout laufen zu lassen. `bridge/fronius_ok` wird dann `0`, und
`bridge/last_poll` bleibt auf dem letzten erfolgreichen Zeitpunkt stehen.
Im Log steht nur der Wechsel (weg / wieder da), nicht jeder Zyklus.

## Konfiguration (`config.json`)

Eine einzige Datei, sowohl von der Web-UI (`api.cgi`) geschrieben als auch
vom Go-Daemon direkt gelesen:

```json
{
  "fronius": {
    "host": "",
    "poll_interval_seconds": 5,
    "http_timeout_seconds": 5,
    "battery_control": {
      "enabled": false, "user": "customer", "password": "", "config_path": "auto",
      "soc_limit": { "enabled": false, "min": 5, "max": 80,
                     "write_interval_minutes": 10, "read_interval_minutes": 10 }
    }
  },
  "mqtt": {
    "use_local_broker": true,
    "host": "", "port": 1883, "username": "", "password": "",
    "topic_prefix": "fronius/", "client_id": "froniusmqtt",
    "enabled_categories": ["inverter", "storage", "meter", "site"]
  }
}
```

`use_local_broker=true`: `api.cgi` löst Host/Port/Zugangsdaten bei jedem
Speichern frisch aus LoxBerrys eigenem `general.json` auf. Passwörter
(`mqtt.password`, `fronius.battery_control.password`) liegen verschlüsselt
vor (AES-256-GCM, Go-Standardbibliothek, Schlüssel lazy neben `config.json`
als `secret.key`, Modus 600) - gleiches Schema wie beim Schwester-Plugin
EaseeMQTT.

## ⚠ Batterie-Control (experimentell, standardmäßig deaktiviert)

Nutzt den **nicht offiziell von Fronius dokumentierten** Endpunkt
`POST /config/timeofuse` (bzw. `/api/config/timeofuse` ab Firmware
1.36.5-1), HTTP-Digest-Auth mit der Fronius-Weboberflächen-Rolle
`customer`. Quelle: Community-Recherche rund um `evcc-io/evcc` (Diskussion
#11711, `templates/definition/meter/fronius-solarapi-v1.yaml`), **kein**
offizielles Fronius-API-Dokument.

- Schreibt beim Aktivieren (`battery/cmd/hold`) ein 0-24-Uhr-Zeitfenster in
  die Wechselrichter-eigene Konfiguration "Energiemanagement ->
  Batteriemanagement" und **überschreibt dabei ein dort ggf. bereits vom
  Nutzer eingerichtetes Zeitfenster**.
- Kann mit einem Fronius-Firmware-Update ohne Vorwarnung brechen, da nicht
  offiziell dokumentiert.
- Digest-Auth: MD5 und SHA-256 (gegen die RFC-7616-Testvektoren geprüft).
  Die Challenge wird aus `WWW-Authenticate` oder `X-WWW-Authenticate`
  gelesen. Login mit SHA-256 und Benutzer `technician` an einem GEN24 mit
  aktueller Firmware bestätigt.
- Retained gesendete `battery/cmd/*`-Nachrichten werden ignoriert, sonst
  würde jeder Reconnect das Kommando erneut auslösen. Die Kommandos in
  Loxone also ohne Retain-Flag senden.
- Der "Login testen"-Button in der Web-UI führt **nur einen lesenden**
  Aufruf aus (prüft die Digest-Zugangsdaten), schreibt nie. Ein
  tatsächlicher Schreibzugriff passiert ausschließlich über die MQTT-
  Kommando-Topics `battery/cmd/hold`/`battery/cmd/release`.
- Für feingranulare Batterie-Steuerung (Lade-/Entladeraten-Limits) ist laut
  derselben Recherche **SunSpec Modbus TCP (Model 124, "storage")** der
  robustere, vom Wechselrichter selbst offiziell unterstützte Weg - dieses
  Plugin deckt das nicht ab.

### Min-SoC-Steuerung (experimentell, standardmäßig deaktiviert)

Liest und setzt "Minimaler Ladezustand" aus der Fronius-Weboberfläche
(Batteriemanagement -> Einstellungen Ladezustand). Ebenfalls ein
undokumentierter Endpunkt der Weboberfläche: `GET`/`POST
/api/config/batteries` (ältere Firmware `/config/batteries`), gleiche
Zugangsdaten wie oben, eigener Schalter. Body und Antwort sind an einem
GEN24 per Browser-Entwicklertools mitgeschnitten - das Plugin schickt
exakt, was auch die Weboberfläche schickt:

```json
{"BAT_M0_SOC_MIN": 28, "BAT_M0_SOC_MODE": "manual"}
```

Nur diese Felder werden geschrieben, die übrige Batterie-Konfiguration
bleibt unverändert. Der Modus wird dabei auf "manual" gestellt, weil der
Mindestwert sonst nicht gilt; damit gilt auch der eingestellte maximale
Ladezustand. Die Antwort listet pro Feld `writeSuccess`, `writeFailure`,
`validationErrors`, `permissionFailure` und `unknownNodes` - meldet das
Gerät dort ein Problem, steht es im Log.

- Welcher Wert sinnvoll ist, entscheidet Loxone (`battery/cmd/soc_min`).
  Das Plugin verwirft Werte außerhalb der eingestellten Grenzen
  (Standard 5-80 %), Nicht-Ganzzahlen und retained Nachrichten.
- Geschrieben wird nur, wenn der Wert vom Gerätewert abweicht, und
  höchstens einmal pro Mindestabstand (Standard 10 min, schont den Flash
  des Wechselrichters). Ein in der Sperrzeit gesendeter Wert wird danach
  nachgeholt, und zwar nur der jeweils letzte.
- Nach jedem Schreiben liest das Plugin zurück und publiziert
  `battery/state/soc_min` und `battery/state/last_write`. Sonst wird alle
  10 min gelesen.
- Ist der Wechselrichter beim Schreiben nicht erreichbar, wird nach dem
  Mindestabstand erneut versucht. Meldet das Gerät einen Fehler, nicht.
- Der Button "Batterie-Konfiguration lesen" in der Web-UI zeigt die erkannten
  Werte und die Rohantwort (rein lesend). Die Rohantwort steht beim ersten
  Lesen nach dem Dienststart außerdem einmal im Log.
- Eine Notstrom-Reserve (nur sichtbar, wenn Notstrom/PV Point eingerichtet
  ist) ist noch nicht eingebaut; das Feld wird nachgerüstet, sobald es an
  einem Gerät mitgeschnitten ist.

## Build/Installation

`postroot.sh` installiert
bei Bedarf eine Go-Toolchain und baut den Daemon aus dem mitgelieferten
`daemon/`-Quellcode direkt auf dem LoxBerry (kein `go.sum` im Repo,
`go mod tidy` löst Abhängigkeiten frisch vom Go-Modul-Proxy auf).

## Lizenz

MIT, siehe [LICENSE](LICENSE). Drittanbieter-Abhängigkeiten und ein
markenrechtlicher Hinweis zum verwendeten Fronius-Logo stehen in
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).


