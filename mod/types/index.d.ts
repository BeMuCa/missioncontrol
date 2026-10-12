// One numbered part of a prompt and the A#n section that answers it, as
// `missioncontrol -json` reports it.
export type Item = { num: number; text: string; answer: string; final: boolean }

// One prompt as `missioncontrol -json` reports it.
export type Prompt = { id: string; text: string; reply: string; done: boolean; items: Item[] }

// One question-and-answer pair on the board: a prompt and the number of its
// part (0 for a prompt that is one pair as a whole).
export type Spot = { prompt: string; num: number }

declare module 'claude-code' {
  interface PluginState {
    missioncontrol: {
      prompts: Prompt[]
      // null follows the answer being written; a spot holds one pair open.
      focus: Spot | null
      // The pair was picked by hand and rides along with the next typed prompt.
      armed: boolean
      // The last A#n the draft in the prompt box names, shown ahead of focus.
      draft: Spot | null
      // Rows scrolled from the start of the pair `key` names.
      scroll: { key: string; delta: number }
      // Where the board sits: above the prompt, or in a pane on the right.
      layout: 'band' | 'side'
      error: string | null
    }
  }
}
