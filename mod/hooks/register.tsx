import { atom, read, update } from 'claude-code'
import type { Elements, EngineInterface, Register, Timer, UiScrollInput } from 'claude-code'

import type { Prompt, Spot } from '../types'
import { buildBoard } from './board'

const prompts = atom({ plugin: 'missioncontrol', key: 'prompts' } as const, [])
const focus = atom({ plugin: 'missioncontrol', key: 'focus' } as const, null)
const armed = atom({ plugin: 'missioncontrol', key: 'armed' } as const, false)
const draft = atom({ plugin: 'missioncontrol', key: 'draft' } as const, null)
const scroll = atom({ plugin: 'missioncontrol', key: 'scroll' } as const, { key: '', delta: 0 })
const error = atom({ plugin: 'missioncontrol', key: 'error' } as const, null)
const placement = atom({ plugin: 'missioncontrol', key: 'layout' } as const, 'band')

const PANE = 'missioncontrol'
// What one $.fs.read may copy; a longer transcript is read on through tail.
const READ_MAX = 4 * 1024 * 1024

// How much of a referenced answer rides along with the next prompt; the whole
// answer is in the conversation already, this only names which one is meant.
const REPLY_CAP = 2000
// The board's own accent (colour 39 in the terminal app), so the band reads as
// MissionControl and not as Claude's.
const ACCENT = '#00afff'
const CODE = '#d7875f'
// Columns of the switcher on the right, and the parts it lists at most.
const SIDE = 10
const ITEMS = 6

// A#3, or P2 A#3 for an older prompt; P2.A#3 and P2/A#3 read the same.
const REF = /(?:\b[Pp](\d+)[\s./:]*)?A#(\d+)/g

type Pair = { num: number; question: string; answer: string; final: boolean }
type Found = { prompt: Prompt; pairs: Pair[]; index: number }
type Span = { text: string; bold?: boolean; color?: string }
type Row = { pair: number; spans: Span[]; isDivider?: boolean }
type Els = Pick<Elements['terminal'], 'Box' | 'Text' | 'Button'>
type Toggle = { label: string; onPress: () => unknown }

let poll: Timer | undefined
// The draft's last reference, so typing without A#n costs no state call.
let draftKey = ''
// What the last drawing showed and how far it could scroll: a wheel tick is
// clamped with it, since only the drawing knows how the answers wrap.
let shown = { key: '', min: 0, max: 0, page: 1 }
let cwd = ''
let transcript: string | undefined
// A transcript past READ_MAX, read up to its last complete line so far.
let cache = { path: '', offset: 0, text: '' }

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    cwd = e.cwd
    await refresh($)
    if ((await read($, placement)) === 'side') {
      void $.ui.open({ id: PANE, title: 'MissionControl' })
    }

    return next(e)
  })

  // A /clear starts another transcript, and no session.start follows it.
  on('session.end', async ($, e, next) => {
    if (e.reason === 'clear') {
      transcript = undefined
      await update($, prompts, () => [])
      await update($, focus, () => null)
    }

    return next(e)
  })

  // The transcript grows while a turn runs; between turns nothing changes.
  on('turn.start', async ($, e, next) => {
    poll ??= $.clock.every(2000, () => void refresh($))

    return next(e)
  })

  on('turn.complete', async ($, e, next) => {
    if (e.agentId === undefined) {
      poll?.cancel()
      poll = undefined
      await refresh($)
      // The turn's last answer can reach the disk after the turn has ended.
      $.clock.after(1500, () => void refresh($))
    }

    return next(e)
  })

  // The band jumps to the pair a draft names before it is sent.
  on('prompt.edit', async ($, e, next) => {
    const box = await next(e)
    const named = box.text.includes('A#') ? refs(box.text, await read($, prompts)).at(-1) : undefined
    const key = named ? spotKey(named) : ''
    if (key !== draftKey) {
      draftKey = key
      await update($, draft, () => named ?? null)
    }

    return box
  }).catch(($, e, next) => next(e))

  on('prompt.submit', async ($, e, next) => {
    if (e.origin.kind !== 'composer') {
      return next(e)
    }
    const list = await read($, prompts)
    const held = await read($, focus)
    const named = refs(e.text, list)
    const meant = named.length > 0 ? named : held !== null && (await read($, armed)) ? [held] : []

    draftKey = ''
    await update($, draft, () => null)
    await update($, armed, () => false)
    // The last reference stays open; a prompt with none goes back to live.
    await update($, focus, () => meant.at(-1) ?? null)

    const context = meant.flatMap(spot => {
      const found = find(list, spot)
      return found ? [reference(found)] : []
    })
    if (context.length === 0) {
      return next(e)
    }

    return next({ ...e, context: [...(e.context ?? []), ...context] })
  }).catch(($, e, next) => next(e))

  // The board scrolls itself: the engine's window would take the question away.
  on('ui.scroll', { component: 'AbovePrompt' }, ownScroll).catch(($, e, next) => next(e))
  on('ui.scroll', { component: 'Pane', requestId: PANE }, ownScroll).catch(($, e, next) => next(e))

  // Closing the pane by its mark puts the board back above the prompt.
  on('ui.close', async ($, e, next) => {
    if (e.id === PANE && e.origin.kind !== 'unload') {
      await update($, placement, () => 'band')
    }

    return next(e)
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const list = await read($, prompts)
    const err = await read($, error)
    if (e.props.hasSurvey || (list.length === 0 && err === null) || (await read($, placement)) === 'side') {
      return next(e)
    }

    const { Box, Text, Button } = $.ui.resolve(e)
    if (list.length === 0) {
      return <Text color="error">missioncontrol: {err}</Text>
    }

    // Half the window, as far as the slot above the prompt allows. In
    // fullscreen the tree is two rows taller than its window: only a band the
    // engine could scroll gets the wheel and the arrow keys, which the
    // ui.scroll hook then spends on its own rows.
    const vp = e.viewport
    const isDocked = vp?.isFullscreen === true
    const height = isDocked ? e.props.maxRows - 1 : Math.min(e.props.maxRows, Math.floor((vp?.rows ?? 24) / 2))
    const toSide = async () => {
      await update($, placement, () => 'side')
      await $.ui.open({ id: PANE, title: 'MissionControl' })
    }

    return board($, { Box, Text, Button }, list, err, e.props.bodyColumns, height, isDocked, { label: '▥ rechts', onPress: toSide })
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const list = await read($, prompts)
    const err = await read($, error)
    const { Box, Text, Button } = $.ui.resolve(e)
    if (list.length === 0) {
      return <Text dimColor>{err === null ? 'missioncontrol: noch keine Prompts' : `missioncontrol: ${err}`}</Text>
    }
    const toBand = async () => {
      await update($, placement, () => 'band')
      await $.ui.close({ id: PANE })
    }

    // As the band: two rows taller than the window, so the wheel reaches it.
    return board($, { Box, Text, Button }, list, err, e.props.bodyColumns, e.props.scroll.bodyRows - 1, true, {
      label: '▤ oben',
      onPress: toBand,
    })
  })
}

// One pair open, its question pinned on top, the switcher on the right:
// the same drawing above the prompt and in the pane.
async function board(
  $: EngineInterface,
  { Box, Text, Button }: Els,
  list: Prompt[],
  err: string | null,
  columns: number,
  height: number,
  isPadded: boolean,
  toggle: Toggle,
) {
  const drafted = await read($, draft)
  const held = await read($, focus)
  const isArmed = await read($, armed)
  const fromDraft = drafted && find(list, drafted)
  const fromFocus = held && find(list, held)
  const at = fromDraft || fromFocus || find(list, live(list))!
  const mode = fromDraft ? 'draft' : fromFocus ? (isArmed ? 'armed' : 'held') : 'live'
  const cur = at.pairs[at.index]!

  const inner = Math.max(4, height - 2)
  const width = Math.max(10, columns - 4 - SIDE - 1)
  const qMax = Math.max(1, Math.min(3, Math.floor(inner / 4)))
  const body = Math.max(1, inner - 1 - qMax)

  const { rows, starts } = layout(at.pairs, width)
  const key = spotKey({ prompt: at.prompt.id, num: cur.num })
  const s = await read($, scroll)
  const base = starts[at.index]!
  // Scrolling may run past the end until the last pair's question is on top,
  // so a short last pair opens at its own question.
  const last = Math.max(0, rows.length - body, starts.at(-1) ?? 0)
  const offset = Math.min(last, Math.max(0, base + (s.key === key ? s.delta : 0)))
  shown = { key, min: -base, max: last - base, page: Math.max(1, body - 1) }

  // The question of the pair at the top row stays put; its divider would
  // only repeat it.
  const top = at.pairs[rows[offset]?.pair ?? at.index]!
  const question = wrap(`${tag(top)} · ${top.question}`, width).slice(0, qMax)
  const from = rows[offset]?.isDivider ? offset + 1 : offset
  const visible = rows.slice(from, from + body + qMax - question.length)

  const pi = list.indexOf(at.prompt)
  const pick = (spot: Spot) =>
    Promise.all([update($, focus, () => spot), update($, armed, () => true)])
  const n = at.pairs[0]!.num === 0 ? 0 : Math.min(ITEMS, at.pairs.length, inner - 5)
  const first = Math.min(Math.max(0, at.index - Math.floor(n / 2)), at.pairs.length - n)

  const band = (
    <Box key="band" flexDirection="row" width={columns} height={height} borderStyle="round" borderColor={ACCENT} paddingX={1}>
      <Box flexDirection="column" flexGrow={1} flexShrink={1} marginRight={1}>
        <Text wrap="truncate-end">
          <Text bold color={ACCENT}>
            {at.prompt.id} · {tag(cur, 'A')}
          </Text>
          {mode === 'live' && <Text dimColor> · live</Text>}
          {mode === 'held' && <Text dimColor> · Referenz</Text>}
          {mode === 'draft' && <Text color="warning"> · im Entwurf referenziert</Text>}
          {mode === 'armed' && <Text color="warning"> · dein nächster Prompt bezieht sich hierauf</Text>}
          {err !== null && <Text color="error"> · {err}</Text>}
        </Text>
        {question.map(line => (
          <Text bold wrap="truncate-end">
            {line}
          </Text>
        ))}
        {visible.map(row => (
          <Text wrap="truncate-end">
            {row.spans.length === 0 ? ' ' : row.spans.map(sp => <Text bold={sp.bold} color={sp.color}>{sp.text}</Text>)}
          </Text>
        ))}
      </Box>
      <Box flexDirection="column" flexShrink={0} width={SIDE}>
        <Button key="layout" plain label={toggle.label} onPress={toggle.onPress} />
        <Box flexDirection="row">
          <Button
            key="prev"
            plain
            label="‹"
            dimColor={pi === 0}
            onPress={() => pi > 0 && pick(firstSpot(list[pi - 1]!))}
          />
          <Text bold color={ACCENT}> {at.prompt.id} </Text>
          <Button
            key="next"
            plain
            label="›"
            dimColor={pi === list.length - 1}
            onPress={() => pi < list.length - 1 && pick(firstSpot(list[pi + 1]!))}
          />
        </Box>
        {at.pairs.slice(first, first + n).map((x, i) => (
          <Button
            key={`#${x.num}`}
            plain
            label={(first + i === at.index ? '▸ #' : '  #') + x.num}
            dimColor={first + i !== at.index}
            onPress={() => pick({ prompt: at.prompt.id, num: x.num })}
          />
        ))}
        <Box flexGrow={1} />
        <Button key="up" plain label="▲" hotkey="k" onPress={() => scrollBy($, -shown.page)} />
        <Button key="down" plain label="▼" hotkey="j" onPress={() => scrollBy($, shown.page)} />
        {mode !== 'live' && (
          <Button
            key="live"
            plain
            label="live"
            onPress={() => Promise.all([update($, focus, () => null), update($, armed, () => false)])}
          />
        )}
      </Box>
    </Box>
  )

  return isPadded ? (
    <Box flexDirection="column">
      {band}
      <Text> </Text>
      <Text> </Text>
    </Box>
  ) : (
    band
  )
}

// A prompt's pairs: each numbered part with its A#n section. A prompt with no
// numbered parts, or one answered without A#n markers, is one pair as a whole.
function pairs(p: Prompt): Pair[] {
  if (p.items.length === 0 || p.items.every(it => it.answer === '')) {
    return [{ num: 0, question: p.text, answer: p.reply, final: p.done }]
  }

  return p.items.map(it => ({ num: it.num, question: it.text, answer: it.answer, final: it.final }))
}

function find(list: Prompt[], spot: Spot): Found | undefined {
  const prompt = list.find(p => p.id === spot.prompt)
  if (prompt === undefined) {
    return undefined
  }
  const ps = pairs(prompt)
  const index = ps.findIndex(x => x.num === spot.num)

  return index < 0 ? undefined : { prompt, pairs: ps, index }
}

// Live is the newest prompt, at the part whose answer is being written.
function live(list: Prompt[]): Spot {
  const p = list.at(-1)!
  const ps = pairs(p)

  return { prompt: p.id, num: (ps.findLast(x => x.answer !== '') ?? ps[0]!).num }
}

function firstSpot(p: Prompt): Spot {
  return { prompt: p.id, num: pairs(p)[0]!.num }
}

// Every A#n in text that names a pair on the board, in order. A bare A#n means
// the newest prompt with a reply: the markers last seen in the chat.
function refs(text: string, list: Prompt[]): Spot[] {
  const answered = list.findLast(p => p.reply !== '')
  const out: Spot[] = []
  for (const m of text.matchAll(REF)) {
    const p = m[1] ? list.find(one => one.id === `P${m[1]}`) : answered
    const spot = p && { prompt: p.id, num: Number(m[2]) }
    if (spot && find(list, spot) && !out.some(one => spotKey(one) === spotKey(spot))) {
      out.push(spot)
    }
  }

  return out
}

function spotKey(spot: Spot): string {
  return `${spot.prompt}#${spot.num}`
}

function tag(x: Pair, letter = 'Q'): string {
  return x.num === 0 ? (letter === 'Q' ? 'Frage' : 'Antwort') : `${letter}#${x.num}`
}

function reference({ prompt, pairs: ps, index }: Found): string {
  const x = ps[index]!
  const name = x.num === 0 ? prompt.id : `${prompt.id} A#${x.num}`
  const answer = x.answer.length > REPLY_CAP ? x.answer.slice(0, REPLY_CAP) + ' …' : x.answer

  return [
    `Der Nutzer bezieht sich in MissionControl auf ${name}.`,
    `Frage (${name}):\n${x.question}`,
    `Antwort (${name}):\n${answer || '(noch keine)'}`,
  ].join('\n\n')
}

// Every pair of the prompt as rows of the band's width, each opened by a
// divider: the band counts rows itself, so it wraps the text itself.
function layout(ps: Pair[], width: number): { rows: Row[]; starts: number[] } {
  const rows: Row[] = []
  const starts: number[] = []
  ps.forEach((x, pair) => {
    starts.push(rows.length)
    const firstLine = x.question.split('\n')[0] ?? ''
    rows.push({ pair, isDivider: true, spans: [{ text: `${tag(x)} · ${firstLine}`, bold: true, color: ACCENT }] })
    const answer = x.answer !== '' ? x.answer : x.final ? '(keine Antwort)' : '◐ wartet auf die Antwort…'
    for (const spans of markdownRows(answer, width)) {
      rows.push({ pair, spans })
    }
    rows.push({ pair, spans: [] })
  })

  return { rows, starts }
}

// Markdown, simplified to rows: headings bold, **bold** and `code` inline,
// fenced code in its own colour, tables left as written.
function markdownRows(text: string, width: number): Span[][] {
  const out: Span[][] = []
  let isCode = false
  for (const line of text.split('\n')) {
    if (/^\s*```/.test(line)) {
      isCode = !isCode
      continue
    }
    if (isCode || /^\s*\|/.test(line)) {
      out.push([{ text: line, color: isCode ? CODE : undefined }])
      continue
    }
    const head = /^#{1,6}\s+(.*)$/.exec(line)
    if (head) {
      for (const part of wrap(head[1]!, width)) {
        out.push([{ text: part, bold: true, color: ACCENT }])
      }
      continue
    }
    let state = { bold: false, code: false }
    for (const part of wrap(line, width)) {
      const spans: Span[] = []
      for (const piece of part.split(/(\*\*|`)/)) {
        if (piece === '**' && !state.code) {
          state = { ...state, bold: !state.bold }
        } else if (piece === '`') {
          state = { ...state, code: !state.code }
        } else if (piece !== '') {
          spans.push({ text: piece, bold: state.bold, color: state.code ? CODE : undefined })
        }
      }
      out.push(spans)
    }
  }

  return out
}

// Word-wraps text to width; a word longer than the width is cut.
function wrap(text: string, width: number): string[] {
  const out: string[] = []
  for (const line of text.split('\n')) {
    let rest = line
    while (rest.length > width) {
      let cut = rest.lastIndexOf(' ', width)
      if (cut <= 0) {
        cut = width
      }
      out.push(rest.slice(0, cut))
      rest = rest.slice(cut).replace(/^ /, '')
    }
    out.push(rest)
  }

  return out
}

async function scrollBy($: EngineInterface, by: number) {
  await update($, scroll, s => {
    const delta = s.key === shown.key ? s.delta : 0

    return { key: shown.key, delta: Math.min(shown.max, Math.max(shown.min, delta + by)) }
  })
}

async function ownScroll($: EngineInterface, e: UiScrollInput) {
  await scrollBy($, e.by)
  // Without next the engine leaves the site undrawn: the hook redraws it.
  $.ui.invalidate('ui.render')

  return {}
}

async function refresh($: EngineInterface) {
  try {
    transcript ??= await locate($)
    // A session has no transcript on disk before its first prompt.
    if (transcript === undefined) {
      return
    }
    const list = buildBoard(await load($, transcript))
    // Polled while a turn runs: an unchanged board is not written, so nothing redraws.
    if (JSON.stringify(list) !== JSON.stringify(await read($, prompts))) {
      await update($, prompts, () => list)
    }
    await update($, error, () => null)
  } catch (err) {
    await update($, error, () => String(err))
  }
}

// The session's transcript: <config>/projects/<cwd with every other character
// a dash>/<id>.jsonl, else the first project folder that holds <id>.jsonl.
async function locate($: EngineInterface): Promise<string | undefined> {
  const id = await $.session.id()
  const home = (await $.env.get('HOME')) ?? (await $.env.get('USERPROFILE')) ?? ''
  const projects = `${(await $.env.get('CLAUDE_CONFIG_DIR')) ?? `${home}/.claude`}/projects`
  const guess = `${projects}/${cwd.replace(/[^A-Za-z0-9]/g, '-')}/${id}.jsonl`
  if (await $.fs.exists(guess)) {
    return guess
  }
  for (const dir of await $.fs.list(projects)) {
    const path = `${projects}/${dir.name}/${id}.jsonl`
    if (dir.kind === 'dir' && (await $.fs.exists(path))) {
      return path
    }
  }

  return undefined
}

// The transcript's text. Up to READ_MAX one read copies it whole; past that it
// is read on from the last complete line through tail, which every Unix has.
async function load($: EngineInterface, path: string): Promise<string> {
  const { size } = await $.fs.stat(path)
  if (size <= READ_MAX) {
    return $.fs.read(path)
  }
  if (cache.path !== path || size < cache.offset) {
    cache = { path, offset: 0, text: '' }
  }
  for (;;) {
    const run = await $.process.run(['tail', '-c', `+${cache.offset + 1}`, path])
    if (run.exitCode !== 0) {
      throw new Error(run.stderr.trim() || `tail: exit ${run.exitCode}`)
    }
    const end = run.stdout.lastIndexOf('\n') + 1
    if (end === 0) {
      break
    }
    const chunk = run.stdout.slice(0, end)
    cache.text += chunk
    cache.offset += new TextEncoder().encode(chunk).length
    if (!run.isStdoutTruncated) {
      break
    }
  }

  return cache.text
}
