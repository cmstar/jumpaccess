import { useEffect, useState } from 'react'

export function TerminalScrollbackInput({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const [draft, setDraft] = useState(String(value))
  const [invalid, setInvalid] = useState(false)
  useEffect(() => { setDraft(String(value)); setInvalid(false) }, [value])

  function save() {
    const lines = Number(draft)
    if (!draft.trim() || !Number.isInteger(lines) || lines < 0 || lines > 50000) {
      setInvalid(true)
      return
    }
    setInvalid(false)
    setDraft(String(lines))
    if (lines !== value) onChange(lines)
  }

  return <div className="terminal-style-row">
    <div>
      <label htmlFor="terminal-scrollback-lines">历史保留行数</label>
      <small className="setting-help" id="terminal-scrollback-lines-help">每个 SSH Tab 独立保留 0～50,000 行历史，0 表示不保留历史。</small>
    </div>
    <div>
      <input id="terminal-scrollback-lines" type="number" min={0} max={50000} step={1} autoComplete="off" value={draft}
        aria-invalid={invalid} aria-describedby={`terminal-scrollback-lines-help${invalid ? ' terminal-scrollback-lines-error' : ''}`}
        onChange={event => { setDraft(event.target.value); setInvalid(false) }} onBlur={save}
        onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); event.currentTarget.blur() } }} />
      {invalid ? <small className="setting-help terminal-scrollback-error" id="terminal-scrollback-lines-error" role="alert">请输入 0～50,000 之间的整数。</small> : null}
    </div>
  </div>
}
