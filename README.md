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

## Benutzung

Im Verzeichnis einer laufenden Claude-Code-Session:

```sh
missioncontrol
```

Ohne `-session` sucht MissionControl die interaktive Session im aktuellen
Verzeichnis; gibt es keine, sagt es das und beendet sich.

| Flag | Wirkung |
|---|---|
| `-session <id>` | eine bestimmte Session beobachten statt der im cwd |
| `-print` | Board einmal als Text rendern und beenden (für Skripte, Pipes, Hooks) |
| `-select <id>` | mit `-print`: eine Zeile vorauswählen (`P3`, `P3.2`, `A7`) |
| `-stacked=false` | Split-Layout statt der rotierten Ansicht |

## Tasten

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
| `q` `ctrl+c` / `esc` | beenden (`esc` schließt zuerst offene Overlays) |

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
