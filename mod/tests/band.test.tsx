import { expect, test } from 'claude-code/testing'
import type { Args, On } from 'claude-code'

const PROPS = {
  hasSurvey: false,
  isWorking: false,
  maxRows: 20,
  bodyColumns: 100,
  scroll: { offset: 0, bodyRows: 19 },
  view: {},
}

const BAND = {
  plugin: 'missioncontrol',
  surface: 'terminal',
  component: 'AbovePrompt',
  requestId: 'band',
  props: PROPS,
  viewport: { columns: 100, rows: 40, isFullscreen: true },
} as const

const human = (id: string, text: string) =>
  JSON.stringify({ type: 'user', promptId: id, timestamp: '2026-10-11T10:00:00Z', origin: { kind: 'human' }, message: { role: 'user', content: text } })
const reply = (text: string) =>
  JSON.stringify({
    type: 'assistant',
    timestamp: '2026-10-11T10:00:05Z',
    message: { role: 'assistant', stop_reason: 'end_turn', content: [{ type: 'text', text }] },
  })
const jsonl = (...rows: string[]) => rows.join('\n') + '\n'

// P1 asks two numbered parts and gets A#1 and A#2; P2 is still waiting.
const TWO = jsonl(
  human('p1', '1. parser bauen\n2. tests starten'),
  reply('A#1\nparser steht\n\nA#2\ntests **grün**'),
  human('p2', 'warum?'),
)
const lines = (letter: string) => Array.from({ length: 30 }, (_, i) => `${letter}${i + 1}`).join('\n')
const LONG = jsonl(human('p1', '1. erste\n2. zweite'), reply(`A#1\n${lines('a')}\n\nA#2\n${lines('b')}`))

// Where the mod looks for session sess-1 started in /tmp.
const PATH = '/home/t/.claude/projects/-tmp/sess-1.jsonl'

// The engine beneath the plugin: the transcript is on disk at PATH, a sent
// prompt is kept in `sent`, an edit is applied to the draft as the composer would.
function engine(on: On, transcript: string, size = transcript.length) {
  const sent: { text: string; context?: readonly string[] }[] = []
  on('session.start', async () => ({ cwd: '/tmp' }))
  on('session.id', async () => ({ value: 'sess-1' }))
  on('env.get', async (_, e) => ({ value: e.name === 'HOME' ? '/home/t' : undefined }))
  on('fs.exists', async (_, e) => ({ value: e.path === PATH }))
  on('fs.stat', async () => ({ value: { kind: 'file' as const, size, mtimeMs: 0, isLink: false } }))
  on('fs.read', async () => ({ value: transcript }))
  on('prompt.submit', async (_, e) => {
    sent.push({ text: e.text, context: e.context })
    return { text: e.text }
  })
  on('prompt.edit', async (_, e) => ({
    text: e.text.slice(0, e.start) + e.inputText + e.text.slice(e.end),
    cursor: e.start + e.inputText.length,
  }))

  return sent
}

const START = { cwd: '/tmp', surface: 'terminal', isInteractive: true } as const

// The kit raises prompt.edit as the composer does, though its type lists only
// the calls a plugin can make.
const edit = ($: unknown, e: Args<'prompt.edit'>) =>
  ($ as { prompt: { edit: (e: Args<'prompt.edit'>) => Promise<unknown> } }).prompt.edit(e)
const typed = (text: string) => ({ text, origin: { kind: 'composer' as const }, wait: false })

test('live follows the newest prompt in a band half the window high, framed in the accent', async ($, on) => {
  engine(on, TWO)
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  expect(await ui.find({ type: 'Text', text: /Frage · warum\?/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /live/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /wartet auf die Antwort/ })).toBeDefined()
  // Two rows short of the slot, then two blank rows: a tree the engine can scroll.
  expect((await ui.find({ key: 'band' }))?.props).toMatchObject({ height: 19, borderStyle: 'round', borderColor: '#00afff' })
  expect((await ui.findAll({ type: 'Text', text: /^ $/ })).length).toBeGreaterThanOrEqual(2)

  // Outside fullscreen the slot is the whole terminal: the band takes half.
  const inline = await $.ui.mount({
    ...BAND,
    requestId: 'inline',
    props: { ...PROPS, maxRows: 30 },
    viewport: { columns: 100, rows: 30, isFullscreen: false },
  })
  expect((await inline.find({ key: 'band' }))?.props).toMatchObject({ height: 15 })
})

test('a draft naming A#2 opens that pair before it is sent, and lets go when the A#2 is gone', async ($, on) => {
  engine(on, TWO)
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  await edit($, { origin: { kind: 'composer' }, text: '', cursor: 0, start: 0, end: 0, inputText: 'zu A#2 bitte' })
  expect(await ui.find({ type: 'Text', text: /P1 · A#2/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Q#2 · tests starten/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'grün' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /im Entwurf/ })).toBeDefined()

  await edit($, { origin: { kind: 'composer' }, text: 'zu A#2 bitte', cursor: 12, start: 0, end: 12, inputText: '' })
  expect(await ui.find({ type: 'Text', text: /live/ })).toBeDefined()
})

test('a sent prompt carries every A#n it names and keeps the last one open until one names none', async ($, on) => {
  const sent = engine(on, TWO)
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  await $.prompt.submit(typed('A#2 passt, aber P1 A#1?'))
  const context = sent.at(-1)?.context ?? []
  expect(context.length).toBe(2)
  expect(context[0]).toContain('Frage (P1 A#2):\ntests starten')
  expect(context[0]).toContain('tests **grün**')
  expect(context[1]).toContain('Frage (P1 A#1):\nparser bauen')
  expect(await ui.find({ type: 'Text', text: /P1 · A#1/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Referenz/ })).toBeDefined()

  await $.prompt.submit(typed('danke'))
  expect(sent.at(-1)?.context).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /live/ })).toBeDefined()
})

test('a pair picked by hand rides along with the next typed prompt, and only that one', async ($, on) => {
  const sent = engine(on, TWO)
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  await ui.press({ key: 'prev' })
  await ui.press({ key: '#2' })
  expect(await ui.find({ type: 'Text', text: /bezieht sich hierauf/ })).toBeDefined()

  // A notification is no prompt of the person's: it neither carries nor spends it.
  await $.prompt.submit({ text: 'agent fertig', origin: { kind: 'task-notification' }, wait: false })
  expect(sent.at(-1)?.context).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /bezieht sich hierauf/ })).toBeDefined()

  await $.prompt.submit(typed('und jetzt?'))
  expect(sent.at(-1)?.context?.[0]).toContain('P1 A#2')
  expect(await ui.find({ type: 'Text', text: /Referenz/ })).toBeDefined()
})

test('scrolling past an answer brings the next question to the top', async ($, on) => {
  engine(on, LONG)
  await $.session.start(START)
  const ui = await $.ui.mount({ ...BAND, props: { ...PROPS, maxRows: 12 } })
  await ui.press({ key: '#1' })
  expect(await ui.find({ type: 'Text', text: /Q#1 · erste/ })).toBeDefined()

  const wheel = (by: number) =>
    $.ui.scroll({ component: 'AbovePrompt', requestId: 'band', offset: 0, by, bodyRows: 10, contentRows: 10, origin: { kind: 'person' } })

  // Divider, a1..a30 and a gap make 32 rows: row 33 is b1, under Q#2.
  await wheel(33)
  expect(await ui.find({ type: 'Text', text: /Q#2 · zweite/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'b1' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'a30' })).toBeUndefined()

  await wheel(-1000)
  expect(await ui.find({ type: 'Text', text: /Q#1 · erste/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'a1' })).toBeDefined()
})

test('a transcript that cannot be read shows why instead of a board', async ($, on) => {
  on('session.start', async () => ({ cwd: '/tmp' }))
  on('session.id', async () => ({ value: 'sess-1' }))
  on('env.get', async (_, e) => ({ value: e.name === 'HOME' ? '/home/t' : undefined }))
  on('fs.exists', async () => ({ value: true }))
  on('fs.stat', async () => ({ deny: 'EACCES: permission denied' }))
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  expect(await ui.find({ type: 'Text', text: /permission denied/ })).toBeDefined()
})

test('a transcript past 4 MiB is read on through tail, up to its last complete line', async ($, on) => {
  engine(on, '', 5 * 1024 * 1024)
  const argv: (readonly string[])[] = []
  on('process.run', async (_, e) => {
    argv.push(e.argv)
    return {
      value: { exitCode: 0, stdout: TWO + '{"type":"us', stderr: '', isStdoutTruncated: false, isStderrTruncated: false },
    }
  })
  await $.session.start(START)
  const ui = await $.ui.mount(BAND)

  expect(argv[0]).toEqual(['tail', '-c', '+1', PATH])
  expect(await ui.find({ type: 'Text', text: /Frage · warum\?/ })).toBeDefined()
})

test('the board moves into a pane on the right and back above the prompt', async ($, on) => {
  engine(on, TWO)
  const opened: string[] = []
  const closed: string[] = []
  on('ui.open', async (_, e) => {
    opened.push(e.id)
    return { value: { isPlaced: true as const } }
  })
  on('ui.close', async (_, e) => {
    closed.push(e.id)
    return { value: undefined }
  })
  await $.session.start(START)
  const band = await $.ui.mount(BAND)

  await band.press({ key: 'layout' })
  expect(opened).toEqual(['missioncontrol'])

  const pane = await $.ui.mount({
    plugin: 'missioncontrol',
    surface: 'terminal',
    component: 'Pane',
    requestId: 'missioncontrol',
    props: { title: 'MissionControl', isFocused: false, bodyColumns: 70, placement: 'dock', scroll: { offset: 0, bodyRows: 40 }, view: {} },
    viewport: { columns: 200, rows: 50, isFullscreen: true },
  })
  expect(await pane.find({ type: 'Text', text: /Frage · warum\?/ })).toBeDefined()
  expect((await pane.find({ key: 'band' }))?.props).toMatchObject({ height: 39, width: 70 })

  await pane.press({ key: 'layout' })
  expect(closed).toEqual(['missioncontrol'])
})

test('live opens a short last pair at its own question, not at the long one before it', async ($, on) => {
  engine(on, jsonl(human('p1', '1. erste\n2. zweite'), reply(`A#1\n${lines('a')}\n\nA#2\nkurz`)))
  await $.session.start(START)
  const ui = await $.ui.mount({ ...BAND, props: { ...PROPS, maxRows: 12 } })

  expect(await ui.find({ type: 'Text', text: /P1 · A#2/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Q#2 · zweite/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'kurz' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Q#1 · erste/ })).toBeUndefined()
})
