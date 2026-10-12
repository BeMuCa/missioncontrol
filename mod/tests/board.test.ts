import { expect, test } from 'claude-code/testing'
import { buildBoard } from '../hooks/board'
import type { Item } from '../types'

// Loosely typed so tests can index results without undefined checks.
const build = (s: string): any[] => buildBoard(s)

// Transcript lines are JSON as they sit in the file, so keep escapes literal.
const r = String.raw
const jl = (...lines: string[]) => lines.join('\n') + '\n'

const prompt1 = r`{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. baue den parser für transcripts\n2. starte agent der die tests prüft"}}`
const reply1 = r`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"mache ich"}]}}`
const done1 = r`{"type":"user","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a"}]}}`
const prompt2 = r`{"type":"user","promptId":"p2","timestamp":"2026-08-25T10:05:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"und jetzt weiter"}}`
const noise = r`{"type":"user","promptId":"p1","isMeta":true,"message":{"role":"user","content":"skill body"}}`

const humanLine = r`{"type":"user","uuid":"u1","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","promptSource":"typed","origin":{"kind":"human"},"message":{"role":"user","content":"1. erste Aufgabe\n2. zweite Aufgabe"}}`
const notifyLine = r`{"type":"user","uuid":"u3","promptId":"p1","promptSource":"system","origin":{"kind":"task-notification"},"message":{"role":"user","content":"agent done"}}`

const userAt = (content: string, id = 'p1') =>
  r`{"type":"user","promptId":"` + id + r`","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"` + content + r`"}}`
const assistant = (text: string, stop?: string) =>
  r`{"type":"assistant","message":{"role":"assistant",` + (stop ? `"stop_reason":"${stop}",` : '') + r`"content":[{"type":"text","text":"` + text + r`"}]}}`

test('groups prompts and items, injected meta entries do not count', async () => {
  const b = build(jl(prompt1, noise, reply1, done1, prompt2))
  expect(b.length).toBe(2)
  expect(b[0].id).toBe('P1')
  expect(b[0].items.length).toBe(2)
  expect(b[0].reply).toBe('mache ich')
  expect(b[0].text).toBe('1. baue den parser für transcripts\n2. starte agent der die tests prüft')
  expect(b[0].items[0]).toEqual({ num: 1, text: 'baue den parser für transcripts', answer: '', final: false })
  expect(b[0].items[1]).toEqual({ num: 2, text: 'starte agent der die tests prüft', answer: '', final: false })
  expect(b[1].items).toEqual([])
})

test('isHumanPrompt: only origin.kind human counts', async () => {
  const meta = r`{"type":"user","uuid":"u2","promptId":"p1","isMeta":true,"message":{"role":"user","content":"Stop hook feedback"}}`
  const result = r`{"type":"user","uuid":"u4","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1"}]}}`
  expect(build(jl(humanLine)).length).toBe(1)
  expect(build(jl(meta, notifyLine, result, reply1)).length).toBe(0)
})

test('text handles both content shapes', async () => {
  const b = build(jl(humanLine, r`{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"hier die Antwort"}]}}`))
  expect(b[0].text).toBe('1. erste Aufgabe\n2. zweite Aufgabe')
  expect(b[0].reply).toBe('hier die Antwort')
})

test('blocks of a reply are joined by blank lines, blank blocks dropped', async () => {
  const two = r`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"eins"},{"type":"tool_use"},{"type":"text","text":"  "},{"type":"text","text":"zwei"}]}}`
  expect(build(jl(prompt2, two))[0].reply).toBe('eins\n\nzwei')
})

test('a trailing partial line is not consumed, unparseable lines are skipped', async () => {
  const partial = r`{"type":"assis`
  expect(build(humanLine + '\n' + reply1 + '\n' + partial)[0].reply).toBe('mache ich')
  expect(build(humanLine + '\n' + partial).length).toBe(1)
  expect(build(jl(humanLine, 'not json', '', reply1))[0].reply).toBe('mache ich')
  expect(build('')).toEqual([])
})

const enqueue = r`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:01:00Z","content":"und noch was dazu"}`
const dequeue = r`{"type":"queue-operation","operation":"remove","timestamp":"2026-08-25T10:02:00Z","content":"und noch was dazu"}`
const waiting = r`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:03:00Z","content":"das wartet noch"}`

test('queued messages become prompts and borrow the reply of the turn they landed in', async () => {
  const b = build(jl(prompt1, enqueue, reply1, dequeue, waiting))
  expect(b.length).toBe(3)
  expect(b.map((p) => p.text)).toEqual([
    '1. baue den parser für transcripts\n2. starte agent der die tests prüft',
    'und noch was dazu',
    'das wartet noch',
  ])
  // The reply belongs to the prompt that was running.
  expect(b[0].reply).toBe('mache ich')
  // The picked-up message shows the reply of the turn it landed in.
  expect(b[1].reply).toBe('mache ich')
  // A message still in the queue has nothing to borrow.
  expect(b[2].reply).toBe('')
  expect(b[1].done).toBe(false)
})

test('a still-pending queued message borrows nothing even with a host reply', async () => {
  const b = build(jl(prompt1, enqueue, reply1))
  expect(b[1].reply).toBe('')
})

test('task notifications are not prompts', async () => {
  const notify = r`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:01:30Z","content":"<task-notification> <task-id>abc</task-id> agent done"}`
  expect(build(jl(prompt1, notify)).length).toBe(1)
})

test('marked answers land on their item', async () => {
  const marked = assistant(r`Vorwort ohne Marker.\n\n[#2] Die Tests laufen jetzt.\nZweite Zeile dazu.\n\n[#1] Der Parser ist gebaut.\n\n[#9] Punkt gibt es nicht.`)
  const p = build(jl(prompt1, marked))[0]
  expect(p.items[0].answer).toBe('Der Parser ist gebaut.')
  expect(p.items[1].answer).toBe('Die Tests laufen jetzt.\nZweite Zeile dazu.')
  expect(p.reply).toContain('Vorwort ohne Marker.')
})

test('unmarked reply leaves items empty', async () => {
  for (const it of build(jl(prompt1, reply1))[0].items) expect(it.answer).toBe('')
})

// Shapes copied out of real transcripts: markers wrapped in emphasis, several to
// a section, and the same text mid-sentence is prose.
test('markers as they actually arrive', async () => {
  const five = userAt(r`1. eins\n2. zwei\n3. drei\n4. vier\n5. fuenf`)
  const reply = assistant(
    r`**[#0]** Vorwort ohne Punkt.\n\n**[#2]** Zwei ist fertig.\n\n**[#3]**, **[#4]** teilen sich diesen Abschnitt.\n\nDazu noch, es kommen **[#1] Doppelcheck** mitten im Satz.\n\n**[#5]** Fuenf zuletzt.`,
  )
  const it = build(jl(five, reply))[0].items
  expect(it[1].answer).toBe('Zwei ist fertig.')
  const shared = 'teilen sich diesen Abschnitt.\n\nDazu noch, es kommen **[#1] Doppelcheck** mitten im Satz.'
  expect(it[2].answer).toBe(shared)
  expect(it[3].answer).toBe(shared)
  expect(it[0].answer).toBe('')
  expect(it[4].answer).toBe('Fuenf zuletzt.')
})

test('marker inside emphasis keeps it balanced', async () => {
  const reply = assistant(r`**[#1] Kein fester Wert.** Der Rest folgt.\n\n**[#2]** Sauber geschlossen.`)
  const it = build(jl(prompt1, reply))[0].items
  expect(it[0].answer).toBe('**Kein fester Wert.** Der Rest folgt.')
  expect(it[1].answer).toBe('Sauber geschlossen.')
})

test('a borrowed reply is not split across items', async () => {
  const q = r`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-26T10:01:00Z","content":"1. erstes ding\n2. zweites ding"}`
  const dq = r`{"type":"queue-operation","operation":"remove","timestamp":"2026-08-26T10:02:00Z","content":"1. erstes ding\n2. zweites ding"}`
  const hostReply = assistant(r`**[#1]** gehoert zum Host.\n\n**[#2]** auch.`)
  const b = build(jl(prompt1, q, hostReply, dq))
  expect(b[1].reply).not.toBe('')
  expect(b[1].reply).toBe(b[0].reply)
  expect(b[1].items.length).toBe(2)
  for (const it of b[1].items) expect(it.answer).toBe('')
  expect(b[0].items[0].answer).toBe('gehoert zum Host.')
})

test('attachment form of a queued message', async () => {
  const att = r`{"type":"attachment","timestamp":"2026-08-26T10:01:00Z","attachment":{"type":"queued_command","prompt":"und dreh das board bitte","commandMode":"prompt","origin":{"kind":"human"}}}`
  const notify = r`{"type":"attachment","timestamp":"2026-08-26T10:01:10Z","attachment":{"type":"queued_command","prompt":"<task-notification> agent fertig","origin":{"kind":"human"}}}`
  const other = r`{"type":"attachment","timestamp":"2026-08-26T10:01:20Z","attachment":{"type":"file","prompt":"nicht getippt"}}`
  const b = build(jl(prompt1, att, notify, other, reply1))
  expect(b.length).toBe(2)
  expect(b[1].text).toBe('und dreh das board bitte')
  expect(b[1].reply).toBe('mache ich')
})

test('dash numbering is recognised', async () => {
  const b = build(jl(userAt(r`1- erstes\n2- zweites\n3) drittes\n4. viertes`)))
  expect(b[0].items.length).toBe(4)
  expect(b[0].items[0].text).toBe('erstes')
})

test('a mid-line number is a part', async () => {
  const it = build(jl(userAt(r`1. erstes\n3. wie trennen wir das? 4. und was ist X?`)))[0].items
  expect(it.length).toBe(3)
  expect(it[1].text).toBe('wie trennen wir das?')
  expect(it[2].text).toBe('und was ist X?')
})

test('a decimal is not split', async () => {
  const b = build(jl(userAt(r`1. der lauf braucht 20.072064 sekunden und 3.5 gb ram`)))
  expect(b[0].items.length).toBe(1)
})

test('answers match the number the user wrote', async () => {
  const p = userAt(r`ist die diagnose richtig? 2. hilft ein websocket? 3. überlappen 7 und 8? 4. was wird akzeptiert?`)
  const a = assistant(r`[#1] ja.\n\n[#2] nein.\n\n[#3] teilweise.\n\n[#4] zahlen.`)
  const it = build(jl(p, a))[0].items
  expect(it.map((i: Item) => [i.num, i.text, i.answer])).toEqual([
    [1, 'ist die diagnose richtig?', 'ja.'],
    [2, 'hilft ein websocket?', 'nein.'],
    [3, 'überlappen 7 und 8?', 'teilweise.'],
    [4, 'was wird akzeptiert?', 'zahlen.'],
  ])
})

test('lead text before a 1 is not an item', async () => {
  const it = build(jl(userAt(r`zwei dinge.\n1. erstes\n2. zweites`)))[0].items
  expect(it.map((i: Item) => i.num)).toEqual([1, 2])
})

test('a quoted number cannot steal an answer', async () => {
  const p = userAt(r`1. eins\n2. zwei\n3. text: \"regel.\n8. Reporting\"\n4. vier`)
  const a = assistant(r`[#3] drei.\n\n[#4] vier.`)
  const it = build(jl(p, a))[0].items
  expect(it.find((i: Item) => i.num === 3)?.answer).toBe('drei.')
  expect(it.find((i: Item) => i.num === 4)?.answer).toBe('vier.')
  expect(it.find((i: Item) => i.num === 8)?.answer ?? '').toBe('')
})

test('done and final follow the turn', async () => {
  const partial = assistant(r`[#1] parser steht.\n\n[#2] tests laufen gerade`, 'tool_use')
  let p = build(jl(prompt1, partial))[0]
  expect(p.done).toBe(false)
  expect(p.items[0].final).toBe(true)
  expect(p.items[1].final).toBe(false)

  const ended = assistant('fertig.', 'end_turn')
  p = build(jl(prompt1, partial, ended))[0]
  expect(p.done).toBe(true)
  expect(p.items[1].final).toBe(true)
  expect(p.items[1].answer.endsWith('fertig.')).toBe(true)

  const b = build(jl(prompt1, partial, prompt2))
  expect(b[0].done).toBe(true)
  expect(b[1].done).toBe(false)
})

test('hash numbering is recognised', async () => {
  const p = userAt(r`#1 die Station bietet vier Knöpfe. Wieso?\n#2 Das Ambiguous gefällt mir nicht. #3 und das hier?\n[#4] noch eins`)
  const a = assistant(r`[#1] Server und Station.\n\n[#3] hier.\n\n[#4] eins.`)
  const it = build(jl(p, a))[0].items
  expect(it.map((i: Item) => [i.num, i.text, i.answer])).toEqual([
    [1, 'die Station bietet vier Knöpfe. Wieso?', 'Server und Station.'],
    [2, 'Das Ambiguous gefällt mir nicht.', ''],
    [3, 'und das hier?', 'hier.'],
    [4, 'noch eins', 'eins.'],
  ])
})

test('a hash in prose is not a part', async () => {
  expect(build(jl(userAt(r`1. schau dir ticket #12 an und PR #3 auch`)))[0].items.length).toBe(1)
})

test('answer marker A#n, with [#n] still accepted', async () => {
  const p = userAt(r`#1 eins\n#2 zwei\n#3 drei\n#4 vier\n#5 fünf`)
  const a = assistant(
    r`Vorwort.\n\nA#1 erste Antwort.\n\n**A#2** zweite.\n\nA#3, A#4 dritte und vierte zusammen.\n\n[#5] alte Form.\n\nSiehe A#1 oben — das ist Prosa, kein Marker.`,
    'end_turn',
  )
  const it = build(jl(p, a))[0].items
  expect(it.map((i: Item) => i.answer)).toEqual([
    'erste Antwort.',
    'zweite.',
    'dritte und vierte zusammen.',
    'dritte und vierte zusammen.',
    'alte Form.\n\nSiehe A#1 oben — das ist Prosa, kein Marker.',
  ])
})

test('sub-numbered markers share the parent block', async () => {
  const a = assistant(r`A#1 der Parser steht.\n\n**A#1.1** die Items zuerst.\n\nA#1.2 dann die Marker.\n\nA#2 die Tests laufen.`, 'end_turn')
  const it = build(jl(prompt1, a))[0].items
  expect(it[0].answer).toBe('der Parser steht.\n\n**A#1.1** die Items zuerst.\n\nA#1.2 dann die Marker.')
  expect(it[0].final).toBe(true)
  expect(it[1].answer).toBe('die Tests laufen.')
})

test('heading markers collect answers', async () => {
  const a = assistant(
    r`## Vorwort A#1 mittendrin ist Prosa.\n\n## A#2 · Tests reviewen\n\nSchritt eins.\n\n## A#1 · Parser\n\nSteht.\n\n## A#1b · Nachtrag\n\nMehr dazu.`,
    'end_turn',
  )
  const it = build(jl(prompt1, a))[0].items
  expect(it[1].answer).toBe('## A#2 · Tests reviewen\n\nSchritt eins.')
  expect(it[0].answer).toBe('## A#1 · Parser\n\nSteht.\n\n## A#1b · Nachtrag\n\nMehr dazu.')
})

test('a parenthesis number is an item, (3) stays a reference', async () => {
  const p = userAt(r`1. eins\n2. zwei, wie in (1) gesagt (5. gibt es ein limit?) und weiter`)
  const a = assistant(r`A#5 kein Limit.`, 'end_turn')
  const it = build(jl(p, a))[0].items
  expect(it.map((i: Item) => i.num)).toEqual([1, 2, 5])
  expect(it[1].text).toContain('wie in (1) gesagt')
  expect(it[1].text).not.toContain('limit')
  expect(it[2].answer).toBe('kein Limit.')
})

test('an orphan answer becomes a question-less item', async () => {
  const a = assistant(r`A#1 eins erledigt.\n\nA#7 sieben dazu.\n\n## A#9 · Neun\n\nneun auch.`, 'end_turn')
  const it = build(jl(prompt1, a))[0].items
  expect(it.length).toBe(4)
  expect(it[2]).toEqual({ num: 7, text: '', answer: 'sieben dazu.', final: true })
  expect(it[3]).toEqual({ num: 9, text: '', answer: '## A#9 · Neun\n\nneun auch.', final: true })
})

test('grouped sub markers land once', async () => {
  const a = assistant(r`A#1.1, A#1.2 beide zusammen.`, 'end_turn')
  expect(build(jl(prompt1, a))[0].items[0].answer).toBe('A#1.1, A#1.2 beide zusammen.')
})

test('json splits numbered parts with their answers', async () => {
  const lines = jl(
    r`{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. parser bauen\n2. tests starten"}}`,
    r`{"type":"assistant","timestamp":"2026-08-25T10:00:05Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A#1\nparser steht\n\nA#2\ntests grün"}]}}`,
  )
  expect(build(lines)).toEqual([
    {
      id: 'P1',
      text: '1. parser bauen\n2. tests starten',
      reply: 'A#1\nparser steht\n\nA#2\ntests grün',
      done: true,
      items: [
        { num: 1, text: 'parser bauen', answer: 'parser steht', final: true },
        { num: 2, text: 'tests starten', answer: 'tests grün', final: true },
      ],
    },
  ])
})
