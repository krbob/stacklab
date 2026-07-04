import { EditorView } from '@codemirror/view'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

// Amber Console editor theme (design tokens v2). Replaces One Dark: the editor
// sits on the warm near-black surface stack, keys carry the accent, values stay
// neutral, and nothing in the highlight palette collides with status colors.
const amberConsoleTheme = EditorView.theme(
  {
    '&': {
      backgroundColor: '#171107',
      color: '#F2EADB',
    },
    '.cm-content': {
      caretColor: '#F5A524',
    },
    '.cm-cursor, .cm-dropCursor': {
      borderLeftColor: '#F5A524',
    },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: 'rgba(245, 165, 36, 0.18)',
    },
    '.cm-activeLine': {
      backgroundColor: 'rgba(245, 165, 36, 0.05)',
    },
    '.cm-gutters': {
      backgroundColor: '#171107',
      color: '#6E6757',
      border: 'none',
      borderRight: '1px solid rgba(245, 165, 36, 0.10)',
    },
    '.cm-activeLineGutter': {
      backgroundColor: 'rgba(245, 165, 36, 0.08)',
      color: '#F5A524',
    },
    '.cm-foldPlaceholder': {
      backgroundColor: 'transparent',
      border: 'none',
      color: '#97907E',
    },
    '.cm-tooltip': {
      backgroundColor: '#201808',
      border: '1px solid rgba(245, 165, 36, 0.13)',
      color: '#F2EADB',
    },
    '.cm-panels': {
      backgroundColor: '#151007',
      color: '#F2EADB',
    },
    '.cm-searchMatch': {
      backgroundColor: 'rgba(245, 165, 36, 0.25)',
      outline: '1px solid rgba(245, 165, 36, 0.4)',
    },
  },
  { dark: true },
)

const amberConsoleHighlight = HighlightStyle.define([
  { tag: [t.propertyName, t.attributeName], color: '#F5A524' },
  { tag: [t.keyword, t.operator, t.definitionKeyword], color: '#E8C07A' },
  { tag: [t.string, t.special(t.string)], color: '#F2EADB' },
  { tag: [t.number, t.bool, t.null], color: '#55D47F' },
  { tag: [t.comment, t.meta], color: '#6E6757', fontStyle: 'italic' },
  { tag: t.invalid, color: '#F26D5B' },
  { tag: [t.separator, t.punctuation, t.bracket], color: '#97907E' },
  { tag: [t.variableName, t.name], color: '#F2EADB' },
])

export const amberConsole = [amberConsoleTheme, syntaxHighlighting(amberConsoleHighlight)]
