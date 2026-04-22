# State preservation

tfreport can carry user-editable content forward across regenerations, so
ticked checkboxes, picked radio options, and reviewer notes survive PR
updates instead of being wiped every push.

## The primitive

A **preserve region** is a span of rendered output whose contents belong
to the human once they've touched it. The wire format is a pair of
HTML comments, invisible in the rendered markdown:

```
<!-- tfreport:preserve-begin id="<id>" kind="<kind>" [attr="..."] -->
<body>
<!-- tfreport:preserve-end id="<id>" -->
```

- `id` is the stable key used to match previous → current across renders.
  Keying by id makes preservation order-agnostic and drift-tolerant: the
  region can move around, others can be added or removed, and the preserved
  content still lands on the right row.
- `kind` declares the shape of the content and drives validation at merge
  time. Recognised kinds: `checkbox`, `radio`, `text`, `block`.
- Extra attributes are kind-specific (`options="a,b,c"` for radio).

The end marker echoes `id` only — if the next `preserve-end` doesn't match
the open tag you get a clear error at parse time.

## Template helpers

Five helpers live in the template engine's function map:

### `preserve <id> <kind> [kind-args...]`

Inline wrap — emits begin + default body + end in one call. Kind dictates
the default body and validates any extra args.

```gotmpl
- {{ preserve "deploy:sub-a" "checkbox" }} sub-a
- {{ preserve "deploy:sub-b" "checkbox" "[x]" }} sub-b (default ticked)
- {{ preserve "approver" "radio" (list "platform" "security" "hold") }}
- {{ preserve "reviewer-note" "text" "placeholder" }}
```

### `preserve_begin <id> <kind>` / `preserve_end <id>`

Paired form — emits only the open/close markers. Required for
`kind="block"`; useful when you want template control flow inside the
region.

```gotmpl
{{ preserve_begin "notes" "block" }}
- line 1
{{ if $showLine2 }}- line 2{{ end }}
- line 3
{{ preserve_end "notes" }}
```

Inline `preserve` with `kind="block"` is rejected at render time with an
error pointing to the paired form.

### `prior <id>`

Returns the prior region body verbatim (including any surrounding
whitespace) or `""` if the id isn't in `ctx.PriorRegions`. Escape hatch for
authors who want custom template-time logic rather than relying on the
built-in kind validators.

```gotmpl
{{- $note := prior "reviewer-note" | default "No review yet." -}}
{{ preserve_begin "reviewer-note" "text" }}{{ $note }}{{ preserve_end "reviewer-note" }}
```

### `has_prior <id>`

Boolean companion to `prior`. Useful for conditionally emitting a whole
block only when it was present on the previous render.

## Recognised kinds

| `kind` | Default body | Valid prior content | On invalid/missing prior |
|--------|--------------|---------------------|--------------------------|
| `checkbox` | `[ ]` (override via extra arg `"[x]"`) | exactly one `[x]`/`[X]` or `[ ]` token | falls back to default |
| `radio` | N-line list of `- [ ] opt` (extra arg: options list; second optional arg: default-selected label) | first `[x]` line whose label is still in the current options list | all unticked |
| `text` | `""` (override via extra arg) | anything | prior body verbatim |
| `block` | author-provided between tags | anything | prior body verbatim |

### Not supported

- `dropdown` — GitHub-flavored markdown does not render `<select>` or
  `<input type="radio">` interactively; the body PATCH flow can't round-trip
  state that never appeared in the source. Use `radio` (multiple checkboxes
  with a pick-one convention) instead.
- `<details>` open/closed state — client-side only, does not round-trip
  through the body.

## CLI flag

```
tfreport --plan-file plan.json --previous-body-file old-body.md --target github-pr-body
```

The `--previous-body-file` flag accepts a path or `-` for stdin. tfreport
parses the file once, makes the regions available to the template via
`ctx.PriorRegions`, renders, then runs a post-render reconciliation pass
that replaces each matching region body with the kind-specific merge of
prior and current.

Unset / empty flag is a no-op — templates that call `preserve` helpers
still emit the markers, but no merging happens.

## Config — `output.preserve_strict`

```yaml
output:
  preserve_strict: false  # default
```

When the prior body has malformed markers:

- `preserve_strict: false` (default) — emits `::warning::tfreport: prior
  body parse failed (...)` to stderr and renders as if no prior was
  supplied. CI continues.
- `preserve_strict: true` — hard-fails with exit 1. Use this for
  compliance flows where silent corruption would be worse than a red
  build.

## CI wiring with `scripts/gh_api.py`

The delivery script includes `--fetch-body` and `--fetch-comment`
subcommands that emit the current PR body / sticky comment body. The
typical wiring:

```yaml
- name: Fetch prior PR body for preserve regions
  run: |
    python3 scripts/gh_api.py --fetch-body \
      --github-token ${{ secrets.GITHUB_TOKEN }} \
      --output old-body.md

- name: Render tfreport with state preservation
  uses: ./.github/action
  with:
    plan-file: plan.json
    previous-body-file: old-body.md
    target: github-pr-body
    output-file: new-snippet.md

- name: Send
  uses: ./.github/action/send
  with:
    content-file: new-snippet.md
    pr-body-marker: TFREPORT
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

Same pattern works for sticky comments — use `--fetch-comment --marker
TFREPORT` and `--target github-pr-comment`.

## Built-in `deploy_checklist preserve="true"`

The default `deploy_checklist` block can opt into preservation without
authoring a template:

```yaml
output:
  targets:
    github-pr-body:
      template: |
        {{ .Title }}

        {{ deploy_checklist "preserve" true }}
```

- The id is auto-derived as `deploy:<slugified label>`. Labels outside
  `[A-Za-z0-9._:-]` are replaced with `-`.
- When `preserve="true"` but `--previous-body-file` was NOT supplied,
  the block silently emits the raw `- [ ]` form (no markers) — preservation
  is meaningless without a prior body, and marker cruft in one-off renders
  is worse than the absence.

## Round-trip example

### Run 1 (no prior body)

Template:

```gotmpl
- {{ preserve "deploy:sub-a" "checkbox" }} **sub-a** ~ [run](https://example/1)
```

Render:

```
- <!-- tfreport:preserve-begin id="deploy:sub-a" kind="checkbox" -->[ ]<!-- tfreport:preserve-end id="deploy:sub-a" --> **sub-a** ~ [run](https://example/1)
```

### Human ticks the checkbox in the PR

PR body now contains:

```
- <!-- tfreport:preserve-begin id="deploy:sub-a" kind="checkbox" -->[x]<!-- tfreport:preserve-end id="deploy:sub-a" --> **sub-a** ~ [run](https://example/1)
```

### Run 2 (new push, prior body fed in)

```
tfreport ... --previous-body-file old-pr-body.md
```

Render:

```
- <!-- tfreport:preserve-begin id="deploy:sub-a" kind="checkbox" -->[x]<!-- tfreport:preserve-end id="deploy:sub-a" --> **sub-a** ~ [run](https://example/2)
```

The `[x]` is preserved; the workflow URL refreshes because it lives outside
the region.
