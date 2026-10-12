// Port of core/transcript + core/board (prompts only, no agents) of the Go
// missioncontrol binary. Pure: takes the transcript text, returns what
// `missioncontrol -json` prints.
import type { Prompt } from '../types'

type Origin = { kind?: string | null } | null
type Block = { type: string; text: string; tool_use_id: string }
type Entry = {
  type: string
  promptId: string
  isMeta: boolean
  origin: Origin
  operation: string
  content: unknown
  attachment: { type: string; prompt: string; origin: Origin } | null
  message: { content: unknown; stop_reason: string }
}

type BItem = { num: number; text: string; answer: string; final: boolean }
type BPrompt = {
  id: string
  text: string
  reply: string
  done: boolean
  queued: boolean
  pending: boolean
  items: BItem[]
}

// Go's \s (RE2) is [\t\n\f\r ], narrower than JS \s; ^ in (?m) is only after \n.
const WS = '[\\t\\n\\f\\r ]'
const LINE = '(?:^|(?<=\\n))'

// A list item the user typed: number at line start, or opening a sentence
// mid-line ("… fertig? 2. dann das"), "#1" / "[#1]", or "(5. " squeezed into
// another part's sentence (dotted shape only).
const itemRE = new RegExp(
  `(?:${LINE}[ \\t]*|[.?!]${WS}+)(?:\\[?#(\\d{1,2})\\]?|(\\d{1,2})[.):-])${WS}+|\\((\\d{1,2})\\.${WS}+`,
  'dg',
)
// A marker run opening a line: "A#4", the older "[#4]", wrapped in emphasis,
// several at once, optional sub-numbers (A#1.1, A#1b), optional Markdown heading.
const runRE = new RegExp(
  `${LINE}[ \\t]*(#{1,6}[ \\t]+)?((?:(?:\\*\\*|__|\\*)?(?:A#\\d{1,2}(?:\\.\\d{1,2})*[a-z]?|\\[#\\d{1,2}(?:\\.\\d{1,2})*[a-z]?\\])(?:\\*\\*|__|\\*)?[,;/&+ \\t]*)+)`,
  'dg',
)
const numRE = /(?:A#|\[#)(\d{1,2})/g
// A sub-marker in a run: a digit followed by a dot or letter.
const subRE = /\d[.a-z]/

// Go's strings.TrimSpace (Unicode White_Space), not JS trim (which also trims
// U+FEFF and misses U+0085).
const SPACE = '\\t\\n\\v\\f\\r \\u0085\\u00A0\\u1680\\u2000-\\u200A\\u2028\\u2029\\u202F\\u205F\\u3000'
const trimRE = new RegExp(`^[${SPACE}]+|[${SPACE}]+$`, 'g')
const goTrim = (s: string): string => s.replace(trimRE, '')

// --- transcript ---------------------------------------------------------

const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v)
// Go's json.Unmarshal fails the whole line on a wrong-typed field; null is fine.
const strOrNull = (v: unknown): boolean => v === undefined || v === null || typeof v === 'string'
const objOrNull = (v: unknown): boolean => v === undefined || v === null || isObj(v)
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/

const str = (v: unknown): string => (typeof v === 'string' ? v : '')

function validOrigin(v: unknown): boolean {
  return objOrNull(v) && (!isObj(v) || strOrNull(v.kind))
}

// Mirrors decoding a line into the Go Entry struct: null when the line would
// not decode (skipped), an entry otherwise.
function parseEntry(line: string): Entry | null {
  let v: unknown
  try {
    v = JSON.parse(line)
  } catch {
    return null
  }
  if (v === null) return emptyEntry()
  if (!isObj(v)) return null
  if (!strOrNull(v.type) || !strOrNull(v.promptId) || !strOrNull(v.operation)) return null
  if (!(v.isMeta === undefined || v.isMeta === null || typeof v.isMeta === 'boolean')) return null
  if (!(v.timestamp === undefined || v.timestamp === null || (typeof v.timestamp === 'string' && RFC3339.test(v.timestamp)))) return null
  if (!validOrigin(v.origin)) return null
  const att = v.attachment
  if (!objOrNull(att)) return null
  if (isObj(att) && !(strOrNull(att.type) && strOrNull(att.prompt) && validOrigin(att.origin))) return null
  const msg = v.message
  if (!objOrNull(msg)) return null
  if (isObj(msg) && !strOrNull(msg.stop_reason)) return null
  return {
    type: str(v.type),
    promptId: str(v.promptId),
    isMeta: v.isMeta === true,
    origin: isObj(v.origin) ? { kind: str(v.origin.kind) } : null,
    operation: str(v.operation),
    content: v.content,
    attachment: isObj(att)
      ? { type: str(att.type), prompt: str(att.prompt), origin: isObj(att.origin) ? { kind: str(att.origin.kind) } : null }
      : null,
    message: { content: isObj(msg) ? msg.content : undefined, stop_reason: isObj(msg) ? str(msg.stop_reason) : '' },
  }
}

function emptyEntry(): Entry {
  return { type: '', promptId: '', isMeta: false, origin: null, operation: '', content: undefined, attachment: null, message: { content: undefined, stop_reason: '' } }
}

// Content is a bare string or a list of blocks; Blocks normalises both. A list
// with a wrong-typed block yields nothing, as in Go.
function blocks(e: Entry): Block[] {
  const raw = e.message.content
  if (raw === undefined) return []
  if (typeof raw === 'string') return [{ type: 'text', text: raw, tool_use_id: '' }]
  if (raw === null) return [{ type: 'text', text: '', tool_use_id: '' }]
  if (!Array.isArray(raw)) return []
  const out: Block[] = []
  for (const b of raw) {
    if (b === null) {
      out.push({ type: '', text: '', tool_use_id: '' })
      continue
    }
    if (!isObj(b) || !strOrNull(b.type) || !strOrNull(b.text) || !strOrNull(b.tool_use_id)) return []
    out.push({ type: str(b.type), text: str(b.text), tool_use_id: str(b.tool_use_id) })
  }
  return out
}

// The entry's readable text, text blocks joined by blank lines.
function text(e: Entry): string {
  const parts: string[] = []
  for (const b of blocks(e)) if (b.type === 'text' && goTrim(b.text) !== '') parts.push(b.text)
  return parts.join('\n\n')
}

// Tool results, injected skill bodies, hook output and agent notices are all
// stored as role "user"; origin.kind is the only thing separating a typed prompt.
function isHumanPrompt(e: Entry): boolean {
  return e.type === 'user' && !e.isMeta && e.origin !== null && e.origin.kind === 'human'
}

// Message of a queue-operation entry; the content is top-level on those.
function queuedText(e: Entry): string {
  return typeof e.content === 'string' ? e.content : ''
}

// A message typed mid-turn, in its attachment shape (written only once the
// session has taken it, so never pending).
function queuedCommandOf(e: Entry): string {
  const a = e.attachment
  if (a === null || a.type !== 'queued_command' || a.origin === null || a.origin.kind !== 'human') return ''
  return a.prompt
}

// --- board --------------------------------------------------------------

function isTaskNotification(s: string): boolean {
  return goTrim(s).startsWith('<task-notification>')
}

function queuedCommand(e: Entry): string {
  const t = queuedCommandOf(e)
  return t !== '' && !isTaskNotification(t) ? t : ''
}

function itemNum(m: RegExpExecArray | RegExpMatchArray): number {
  for (const g of [m[1], m[2], m[3]]) if (g !== undefined) return parseInt(g, 10)
  return 0
}

function splitItems(src: string): BItem[] {
  const locs = [...src.matchAll(itemRE)]
  const items: BItem[] = []
  // A lead typed before numbering starts at 2 is part number one.
  if (locs.length > 0) {
    const first = itemNum(locs[0]!)
    if (first >= 2) {
      // A mid-line marker matches its leading ".?!", the lead's own sentence end.
      let end = locs[0]!.index!
      if (end < src.length && '.?!'.includes(src[end]!)) end++
      const lead = goTrim(src.slice(0, end))
      if (lead !== '') items.push({ num: first - 1, text: lead, answer: '', final: false })
    }
  }
  locs.forEach((m, i) => {
    // A part runs to the next marker; that marker's leading ".?!" stays with
    // this part.
    let end = src.length
    if (i + 1 < locs.length) {
      let nb = locs[i + 1]!.index!
      if (nb < src.length && '.?!'.includes(src[nb]!)) nb++
      end = nb
    }
    const body = goTrim(src.slice(m.index! + m[0].length, end))
    if (body === '') return
    items.push({ num: itemNum(m), text: body, answer: '', final: false })
  })
  return items
}

// The emphasis a marker run opens but never closes.
function openEmphasis(run: string): string {
  const t = run.replace(/[,;/&+ \t]+$/, '')
  if (t.endsWith('*') || t.endsWith('_')) return ''
  for (const o of ['**', '__', '*']) if (run.startsWith(o)) return o
  return ''
}

// Hands each marked section of the reply to the item it names. The reply itself
// stays whole.
function splitReply(p: BPrompt): void {
  if (p.reply === '' || p.items.length === 0) return
  const byNum = new Map<number, BItem>()
  for (const it of p.items) if (!byNum.has(it.num)) byNum.set(it.num, it)
  const runs = [...p.reply.matchAll(runRE)]
  runs.forEach((r, i) => {
    const start = r.index!
    const mEnd = start + r[0].length
    let end = p.reply.length
    let final = p.done
    if (i + 1 < runs.length) {
      end = runs[i + 1]!.index!
      final = true
    }
    const run = r[2]!
    // A sub-numbered section keeps its marker and appends; a heading keeps its
    // whole line.
    const sub = subRE.test(run)
    const heading = r[1] !== undefined
    let section: string
    if (sub || heading) {
      section = goTrim(p.reply.slice(start, end))
    } else {
      section = goTrim(p.reply.slice(mEnd, end))
      // `**[#3] the answer**`: the opener went with the label, put it back.
      const op = openEmphasis(run)
      if (op !== '') section = op + section
    }
    if (section === '') return
    // A grouped run of siblings ("A#1.1, A#1.2") names the same item twice.
    const seen = new Set<number>()
    for (const num of run.matchAll(numRE)) {
      const n = parseInt(num[1]!, 10)
      if (seen.has(n)) continue
      seen.add(n)
      let it = byNum.get(n)
      if (it === undefined) {
        // A number no typed part carries becomes an item of its own, no question text.
        it = { num: n, text: '', answer: '', final: false }
        byNum.set(n, it)
        p.items.push(it)
      }
      if (sub && it.answer !== '') it.answer += '\n\n' + section
      else it.answer = section
      it.final = final
    }
  })
}

export function buildBoard(jsonl: string): Prompt[] {
  // The Go reader only consumes complete lines; a trailing partial line is
  // left for later. Lines that do not decode are skipped.
  const lines = jsonl.split('\n')
  lines.pop()
  const entries: Entry[] = []
  for (const l of lines) {
    const e = parseEntry(l)
    if (e !== null) entries.push(e)
  }

  // A queued message is recorded when typed and when picked up; what is
  // enqueued and never removed is still waiting.
  const removed = new Map<string, number>()
  for (const e of entries) {
    if (e.type === 'queue-operation' && e.operation === 'remove') {
      const t = queuedText(e)
      removed.set(t, (removed.get(t) ?? 0) + 1)
    }
  }

  const prompts: BPrompt[] = []
  // Each queued message is paired with the turn running when it arrived.
  const landed = new Map<BPrompt, BPrompt | null>()
  let cur: BPrompt | null = null
  for (const e of entries) {
    if (e.type === 'queue-operation' && e.operation === 'enqueue' && isTaskNotification(queuedText(e))) {
      // An agent reporting back is not something the user asked for.
    } else if (e.type === 'queue-operation' && e.operation === 'enqueue') {
      // A queued message does not become the current prompt: its reply is
      // interleaved into the running turn, and splitting that would be a guess.
      const t = queuedText(e)
      const q: BPrompt = { id: `P${prompts.length + 1}`, text: t, reply: '', done: false, queued: true, pending: false, items: [] }
      const n = removed.get(t) ?? 0
      if (n > 0) removed.set(t, n - 1)
      else q.pending = true
      q.items = splitItems(q.text)
      landed.set(q, cur)
      prompts.push(q)
    } else if (queuedCommand(e) !== '') {
      const q: BPrompt = { id: `P${prompts.length + 1}`, text: queuedCommand(e), reply: '', done: false, queued: true, pending: false, items: [] }
      q.items = splitItems(q.text)
      landed.set(q, cur)
      prompts.push(q)
    } else if (isHumanPrompt(e)) {
      if (cur !== null) cur.done = true
      cur = { id: `P${prompts.length + 1}`, text: text(e), reply: '', done: false, queued: false, pending: false, items: [] }
      cur.items = splitItems(cur.text)
      prompts.push(cur)
    } else if (e.type === 'assistant' && cur !== null) {
      // Assistant turns carry no promptId: a reply belongs to the last prompt seen.
      cur.done = e.message.stop_reason === 'end_turn'
      const t = text(e)
      if (t !== '') {
        if (cur.reply !== '') cur.reply += '\n\n'
        cur.reply += t
      }
    }
  }

  for (const p of prompts) splitReply(p)

  // Filled after splitting and never split itself: the numbers in a borrowed
  // reply belong to the prompt it was written for.
  for (const [q, host] of landed) {
    if (q.reply === '' && !q.pending && host !== null && host.reply !== '') q.reply = host.reply
  }

  return prompts.map((p) => ({
    id: p.id,
    text: p.text,
    reply: p.reply,
    done: p.done,
    items: p.items.map((it) => ({ num: it.num, text: it.text, answer: it.answer, final: it.final })),
  }))
}
