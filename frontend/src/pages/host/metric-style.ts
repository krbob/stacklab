export function utilizationTone(value: number, normalBar = 'bg-[var(--accent)]', normalLine = 'var(--accent)') {
  if (value >= 90) {
    return {
      bar: 'bg-[var(--danger)]',
      line: 'var(--danger)',
      text: 'text-[var(--danger)]',
    }
  }
  if (value >= 80) {
    return {
      bar: 'bg-[var(--warning)]',
      line: 'var(--warning)',
      text: 'text-[var(--warning)]',
    }
  }
  return {
    bar: normalBar,
    line: normalLine,
    text: 'text-[var(--text)]',
  }
}
