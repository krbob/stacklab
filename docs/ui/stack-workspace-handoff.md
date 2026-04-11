# Stack Workspace UI Handoff

This handoff covers auxiliary files under one stack root:

- `stacks/<stack_id>/Dockerfile`
- nested config files
- helper text files that live next to the stack definition

It does **not** replace the dedicated editor for:

- `compose.yaml`
- root `.env`

## Product Intent

Operators should be able to:

- inspect helper files that affect one stack
- edit supported text files without SSH
- keep using the dedicated Compose editor for the canonical definition

## Main IA Question

This belongs inside the stack surface, not as a global workspace.

Need UI input:

- should this be:
  - a new `Files` tab under `/stacks/:id/*`
  - or an expanded mode inside the existing `Editor`

The backend contract supports either.

## Recommended v1 Layout

Desktop:

- left file tree
- right file viewer/editor

States:

- empty root
- text file
- binary/unknown read-only
- blocked file
- reserved canonical file explanation

## Important UX Rules

- `compose.yaml` and root `.env` must not be editable through this surface
- if the user reaches a reserved canonical file path indirectly, show:
  - "Use the stack editor for compose.yaml and root .env."
- blocked files should reuse the existing blocked-file card model from config workspace
- save action should map to `save_stack_file`

## Additional UI Decisions Needed

1. If this is a new tab, should `compose.yaml` and root `.env` still appear in the tree as disabled entries, or be omitted entirely.
2. Should nested `.env` files be treated like normal text files:
   - recommendation: yes
3. Should `Dockerfile` get a lightweight special badge or icon:
   - recommendation: yes
