# Third-Party Notices

Der `froniusmqtt`-Daemon (`daemon/`) bindet folgende Open-Source-Bibliothek
statisch ein. Diese Datei listet die direkte Abhängigkeit aus
`daemon/go.mod` mitsamt Lizenz; der Lizenztext ist beim Originalprojekt
verlinkt.

| Modul | Version | Lizenz | Projekt |
|---|---|---|---|
| github.com/eclipse/paho.mqtt.golang | v1.5.1 | EPL-2.0 / EDL-1.0 (dual, Eclipse Foundation) | https://github.com/eclipse/paho.mqtt.golang |

Die HTTP-Digest-Authentifizierung für das optionale Batterie-Control-Feature
(`internal/fronius/control.go`) ist eine minimale eigene Implementierung
(RFC 2617/7616, nur Go-Standardbibliothek) - keine zusätzliche Abhängigkeit.

Diese Lizenz erlaubt uneingeschränkte Nutzung/Weitergabe (auch in
kompilierter Binärform), solange Copyright-Hinweis und Lizenztext erhalten
bleiben.

## Hinweis zu transitiven Abhängigkeiten

Da `go.sum` bewusst nicht im Repo committet ist (wird bei jeder Installation
frisch per `go mod tidy` aufgelöst, siehe `postroot.sh`), lässt sich die
vollständige transitive Abhängigkeitskette hier nicht statisch auflisten.
Bei Bedarf lokal nachvollziehbar mit:

```
cd daemon && go mod tidy && go list -m all
```

## Perl-Frontend (`webfrontend/htmlauth/`)

Nutzt ausschliesslich LoxBerry-eigene Perl-Module (`LoxBerry::System`,
`LoxBerry::Web`) sowie Perl-Core-/Standard-Module (`CGI`, `JSON::PP`,
`strict`, `warnings`) - keine zusaetzlichen Drittanbieter-Abhaengigkeiten,
daher hier nicht gesondert aufgefuehrt.

## Markenrechtlicher Hinweis: Fronius-Logo (`icons/`)

Die Plugin-Icons (`icons/icon_*.png`) zeigen das offizielle Fronius-Logo.
"Fronius" sowie das Fronius-Logo sind eingetragene Marken der
**Fronius International GmbH** (Sitz: Pettenbach, Österreich). Die
Verwendung hier dient **ausschließlich der Produktkennzeichnung** (dieses
Plugin verbindet sich mit einem Fronius-Wechselrichter) und stellt
**keine** Behauptung einer offiziellen Zusammenarbeit, Empfehlung oder
Lizenzierung durch Fronius dar. Dieses Projekt ist ein privates
Community-Plugin, nicht von Fronius International GmbH entwickelt,
autorisiert oder unterstützt. Alle Rechte am Logo verbleiben bei Fronius
International GmbH. Bei Beanstandung durch den Markeninhaber wird das
Icon umgehend entfernt/ersetzt.
