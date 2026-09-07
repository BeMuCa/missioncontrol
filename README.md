# MissionControl

**Claude schreibt Romane. Du wolltest eine Antwort.**

MissionControl ist ein TUI, das eine laufende Claude-Code-Session auf das
reduziert, was du wissen wolltest: deine eigenen Prompts, die numerierten
Teilfragen darin — und was davon schon beantwortet ist.

Kein Scrollen durch Denk-Monologe, Tool-Logs und Zwischenberichte. Du siehst
deine Frage, und du siehst die Antwort. Der Rest bleibt, wo er hingehört.

## Was es zeigt

Der Board-Baum hat drei Ebenen, direkt aus dem Session-Transcript gelesen:

- **Prompts** — was du getippt hast, in der Reihenfolge, in der du es getippt hast.
- **Items** — die numerierten Teile eines Prompts. `1. mach X`, `2) dann Y`,
  `3- und Z` werden alle erkannt, auch mitten in der Zeile (`… fertig? 2. dann das`),
  damit deine Nummerierung nicht darüber entscheidet, ob das Board deine Teile sieht.
- **Agents** — welche Subagents ein Prompt gestartet hat und welche davon fertig sind.

Dazu: Favoriten für Antworten, die du behalten willst, und Done-Marker für Teile,
die abgehakt sind.

## Installation

```sh
go install github.com/BeMuCa/missioncontrol/cmd/missioncontrol@latest
```

Gebaut gegen Go 1.26.

## Von überall starten

Ein Projekt einmal anmelden:

```sh
cd ~/git/meinprojekt
missioncontrol init
```

Danach startet `missioncontrol` von jedem Verzeichnis aus im Projekt-Launcher:
Logo, Wordmark, darunter jedes angemeldete Projekt mit Sessionzahl und letzter
Aktivität. Der Schriftzug steht einzeilig und braucht dafür 85 Spalten; darunter
zeigt der Launcher den Namen als schlichten Text. Enter öffnet das Board auf der neuesten Session, `esc` führt vom Board
zurück zur Liste.

Wurde `missioncontrol` in einem angemeldeten Projekt gestartet, steht die
Auswahl schon darauf — Enter genügt.

Die Registrierung liegt in `~/.missioncontrol/projects.json`, neben `~/.claude`.
Sie enthält nichts als Pfade: die Sessions eines Projekts werden aus seinem Pfad
gefunden, nicht aus der Datei. Die Datei zu löschen kostet nur die Abkürzungen.
`missioncontrol forget` nimmt das aktuelle Verzeichnis wieder heraus.

## Benutzung

```sh
missioncontrol              # Launcher: Projekt wählen, dann Board
missioncontrol -here        # Launcher überspringen, Session in diesem Verzeichnis
missioncontrol init         # dieses Verzeichnis anmelden
missioncontrol forget       # dieses Verzeichnis wieder abmelden
```

| Flag | Wirkung |
|---|---|
| `-here` | Launcher überspringen und die Session im aktuellen Verzeichnis öffnen |
| `-session <id>` | eine bestimmte Session beobachten |
| `-print` | Board einmal als Text rendern und beenden (für Skripte, Pipes, Hooks) |
| `-select <id>` | mit `-print`: eine Zeile vorauswählen (`P3`, `P3.2`, `A7`) |
| `-stacked=false` | Split-Layout statt der rotierten Ansicht |

`-here`, `-session` und `-print` benennen jeweils genau ein Board und gehen
deshalb am Launcher vorbei — eine Pipe hat niemanden, der aus einer Liste wählt.

## Tasten

**Launcher**

| Taste | Aktion |
|---|---|
| `j` `k` / `↑` `↓` | Projekt wechseln |
| `enter` | Board auf der neuesten Session öffnen |
| `r` | Liste neu einlesen |
| `g` `G` | an den Anfang / an das Ende |
| `q` `esc` | beenden |

**Board**

| Taste | Aktion |
|---|---|
| `j` `k` / `↑` `↓` | Zeile bzw. Box wechseln, im Textbereich scrollen |
| `h` `l` / `←` `→` | Prompt wechseln (stacked) bzw. zwischen Baum und Text springen (split) |
| `enter` | Box auf/zu; in der Favoritenansicht: Pfeiltasten an den Text übergeben |
| `t` | Layout umschalten (stacked ↔ split) |
| `m` | alle Boxen auf/zu (stacked) |
| `a` | Agents ein-/ausblenden |
| `f` | Favorit setzen/entfernen (in der Favoritenansicht: löschen) |
| `F` | Favoritenansicht öffnen/schließen |
| `d` | als erledigt markieren |
| `s` | Sessionliste öffnen |
| `r` | neu laden |
| `g` `G` | an den Anfang / an das Ende |
| `ctrl+d` `ctrl+u` | halbseitig scrollen |
| `q` `ctrl+c` | beenden |
| `esc` | offene Overlays schließen, sonst zurück zum Launcher (bzw. beenden, wenn ohne Launcher gestartet) |

## Wie es liest

Ein Session-Transcript ist eine JSONL-Datei, an die während der Session
angehängt wird; jeder Subagent bekommt seine eigene JSONL daneben. MissionControl
liest inkrementell per Byte-Offset — die ganze Datei bei jedem Poll neu zu parsen
würde mit der Sessionlänge skalieren, und Sessions laufen stundenlang.

Zwei Dinge sind exakt aus den Daten ableitbar: zu welchem Prompt ein Agent gehört
(über die `promptId`) und ob er fertig ist (ein `tool_result` für seine
`toolUseId` existiert). Zwei sind es nicht: welchem numerierten Item ein Agent
dient, steht nirgends in den Daten — und ein Agent, dessen `promptId` zu keinem
getippten Prompt gehört, wird über die Zeit einsortiert.
