import { useEffect, useRef } from 'react'
import { EditorView, keymap } from '@codemirror/view'
import { Annotation, EditorState } from '@codemirror/state'
import { basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { indentWithTab } from '@codemirror/commands'
import { amberConsole } from '@/lib/editor-theme'

interface YamlEditorProps {
  value: string
  onChange: (value: string) => void
  readOnly?: boolean
}

const externalValueSync = Annotation.define<boolean>()

export function YamlEditor({ value, onChange, readOnly = false }: YamlEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  useEffect(() => {
    if (!containerRef.current) return

    const state = EditorState.create({
      doc: value,
      extensions: [
        basicSetup,
        yaml(),
        amberConsole,
        keymap.of([indentWithTab]),
        EditorView.updateListener.of((update) => {
          const externalUpdate = update.transactions.some((transaction) => transaction.annotation(externalValueSync))
          if (update.docChanged && !externalUpdate) {
            onChangeRef.current(update.state.doc.toString())
          }
        }),
        EditorState.readOnly.of(readOnly),
        EditorView.theme({
          '&': { height: '100%', fontSize: '14px' },
          '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)' },
          '.cm-content': { padding: '8px 0' },
        }),
      ],
    })

    const view = new EditorView({ state, parent: containerRef.current })
    viewRef.current = view

    return () => {
      view.destroy()
      viewRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readOnly])

  // Update content when external value changes (e.g. after save)
  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const current = view.state.doc.toString()
    if (current !== value) {
      view.dispatch({
        changes: { from: 0, to: current.length, insert: value },
        annotations: externalValueSync.of(true),
      })
    }
  }, [value])

  return (
    <div
      ref={containerRef}
      className="h-full min-w-0 max-w-full overflow-hidden rounded border border-[var(--panel-border)]"
    />
  )
}
