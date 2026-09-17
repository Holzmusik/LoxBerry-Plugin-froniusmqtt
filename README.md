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

1. Fragt bei jedem Poll-Zyklus (konfigurierbar, Default 5s) zuerst
   `GetActiveDeviceInfo.cgi` ab (Geräte-Discovery - Wechselrichter/
   Speicher/Zähler-IDs, **nicht hartcodiert**: neu angeschlossene Geräte
   erscheinen ohne Neustart des Diensts automatisch)
2. Ruft für jedes gefundene Gerät die passenden Detail-Endpunkte ab
   (`GetInverterRealtimeData.cgi`, `GetStorageRealtimeData.cgi`,
   `GetMeterRealtimeData.cgi`, `GetPowerFlowRealtimeData.fcgi`)
3. Übersetzt jedes Feld 1:1 -> retained MQTT-State-Topic
4. Optional (standardmäßig **deaktiviert**, siehe Warnung unten): abonniert
   `battery/cmd/hold` und `battery/cmd/release` -> schreibt eine
   Entladesperre über einen undokumentierten Fronius-Endpunkt

Keine eigene Lastmanagement-/Automatisierungslogik - die bleibt vollständig
in Loxone.

## MQTT-Topic-Schema

Präfix konfigurierbar (Default `fronius/`). Kategorien (Wechselrichter/
Speicher/Zähler/Anlage) einzeln in der Web-UI ("Daten"-Seite) ab-/anwählbar.

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

# nur wenn Batterie-Control aktiviert:
<prefix>battery/cmd/hold                 (Entladesperre aktivieren, Payload beliebig)
<prefix>battery/cmd/release              (Freigabe/Normalbetrieb, Payload beliebig)
<prefix>battery/state/control_mode       (normal|hold, retained - Rückmeldung)
```

`<id>` entspricht der von `GetActiveDeviceInfo.cgi` gemeldeten Geräte-ID
(beim Referenzgerät: 1 Wechselrichter, 2 Zähler, 1 Speicher). Werte, die
Fronius als JSON `null` liefert (z.B. `day_energy` direkt nach Mitternacht,
bevor der erste Tageswert existiert), werden bewusst **nicht** publiziert
statt fälschlich als `0` - retained MQTT behält ohnehin den letzten
bekannten Wert.

## Konfiguration (`config.json`)

Eine einzige Datei, sowohl von der Web-UI (`api.cgi`) geschrieben als auch
vom Go-Daemon direkt gelesen:

```json
{
  "fronius": {
    "host": "",
    "poll_interval_seconds": 5,
    "http_timeout_seconds": 5,
    "battery_control": { "enabled": false, "user": "customer", "password": "", "config_path": "auto" }
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
- Der "Login testen"-Button in der Web-UI führt **nur einen lesenden**
  Aufruf aus (prüft die Digest-Zugangsdaten), schreibt nie. Ein
  tatsächlicher Schreibzugriff passiert ausschließlich über die MQTT-
  Kommando-Topics `battery/cmd/hold`/`battery/cmd/release`.
- Für feingranulare Batterie-Steuerung (Lade-/Entladeraten-Limits, Reserve-
  SOC) ist laut derselben Recherche **SunSpec Modbus TCP (Model 124,
  "storage")** der robustere, vom Wechselrichter selbst offiziell
  unterstützte Weg - dieses Plugin deckt das nicht ab.

## Build/Installation

`postroot.sh` installiert
bei Bedarf eine Go-Toolchain und baut den Daemon aus dem mitgelieferten
`daemon/`-Quellcode direkt auf dem LoxBerry (kein `go.sum` im Repo,
`go mod tidy` löst Abhängigkeiten frisch vom Go-Modul-Proxy auf).

## Lizenz

MIT, siehe [LICENSE](LICENSE). Drittanbieter-Abhängigkeiten und ein
markenrechtlicher Hinweis zum verwendeten Fronius-Logo stehen in
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).


