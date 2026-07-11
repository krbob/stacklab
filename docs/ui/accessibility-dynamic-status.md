# Dynamic Interface Accessibility

This document defines the accessibility contract for asynchronous and stateful Stacklab UI. It applies to background jobs, action feedback, loading regions, progress indicators, view switches, and the command palette.

## Announcements

- Announce one concise, human-readable summary when a job or action changes state.
- Use `role="status"`, `aria-live="polite"`, and `aria-atomic="true"` for non-blocking updates. Action errors use the same polite channel unless immediate intervention is required.
- Keep continuously appended logs and event dumps outside live regions with `aria-live="off"`. The state summary announces progress; the full stream remains available for inspection.
- Do not place elapsed timers inside live regions. Time may update visually without causing repeated announcements.
- Decorative state dots, spinners, and progress fills are hidden from the accessibility tree when adjacent text or ARIA values already expose their meaning.
- Reserve `role="alert"` or assertive announcements for blocking or safety-critical failures. Do not mirror the same message in multiple live regions.

## Loading and progress

- A region that is fetching or replacing its content exposes `aria-busy="true"` and clears it when the new content is ready.
- A running background job is not automatically a busy region. Its status must remain announceable while the job runs.
- Determinate progress uses `role="progressbar"` with an accessible label, `aria-valuemin`, `aria-valuemax`, `aria-valuenow`, and an `aria-valuetext` summary where counts add useful context.
- Skeletons are decorative. A short screen-reader-only loading status identifies what is being fetched.

## Stateful controls

- Independent toggle buttons expose `aria-pressed` according to their current state.
- Mutually exclusive views use the tabs pattern: a labelled `tablist`, `tab` controls with `aria-selected` and `aria-controls`, and associated `tabpanel` elements.
- Tabs use roving focus. Arrow keys move between tabs; `Home` and `End` select the first and last tab.

## Command palette

The command palette is a modal dialog with focus restoration. Search is exposed as a `combobox` controlling a `listbox`; the active result is identified through `aria-activedescendant`, and every result is an `option` with selection state.

Keyboard behavior:

- `Ctrl+K` or `Cmd+K` opens the palette and focuses search.
- Arrow keys, `Home`, and `End` move the active result.
- `Enter` activates the current result.
- `Escape` closes the palette, and focus returns to the element that opened it.
- `Tab` remains inside the palette while it is open.

The result count is announced politely and atomically. Empty results are represented by the same status channel rather than an assertive alert.

## Verification

React Testing Library coverage queries the exposed roles and state attributes for statuses, busy regions, progress bars, toggles, tabs, job details, and the command palette. Keyboard tests cover tab navigation, result selection, focus containment, and focus restoration. The current frontend test toolchain does not include an automated axe runner; semantic role assertions are the repository-level regression gate until one is added.
