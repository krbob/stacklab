import { act, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { YamlEditor } from './yaml-editor'

describe('YamlEditor', () => {
  it('does not emit onChange for external value syncs', async () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <div style={{ height: 300 }}>
        <YamlEditor value={'services:\n  app:\n    image: nginx\n'} onChange={onChange} />
      </div>,
    )

    expect(onChange).not.toHaveBeenCalled()

    rerender(
      <div style={{ height: 300 }}>
        <YamlEditor value={'services:\n  app:\n    image: caddy\n'} onChange={onChange} />
      </div>,
    )
    await act(async () => {})

    expect(onChange).not.toHaveBeenCalled()
  })
})
