# Custom Tools

You can add your own tools, override a built-in tool, or remove a built-in
tool entirely. Create `~/.ccodolo/custom-tools.json`. ccodolo reads this
file on every invocation. If the file does not exist, nothing happens. If
the file exists but fails to parse, ccodolo prints a warning to stderr and
ignores the file. Your build still proceeds with the built-in catalog.

## File format

```json
{
  "ignore": ["ruby", "php"],
  "tools": [
    {
      "name": "htop",
      "description": "Interactive process viewer",
      "instructions": [
        "RUN apt update && apt install -y --no-install-recommends htop && rm -rf /var/lib/apt/lists/*"
      ]
    },
    {
      "name": "internal-cli",
      "description": "Acme internal CLI",
      "source_image": "registry.acme.example/tools/internal-cli",
      "default_tag": "1.2.3",
      "instructions": [
        "COPY --from=%s /usr/bin/internal-cli /usr/local/bin/internal-cli"
      ]
    },
    {
      "name": "python",
      "description": "Python 3.11 (corporate-pinned)",
      "source_image": "public.ecr.aws/docker/library/python",
      "default_tag": "3.11",
      "tag_suffix": "-slim",
      "instructions": [
        "COPY --from=%s /usr/local/lib/python{{.Version}} /usr/local/lib/python{{.Version}}",
        "COPY --from=%s /usr/local/bin/python3* /usr/local/bin/",
        "COPY --from=%s /usr/local/bin/pip* /usr/local/bin/",
        "RUN ln -sf /usr/local/bin/python3 /usr/local/bin/python"
      ]
    }
  ]
}
```

Top-level keys:

| Key | Type | Description |
|-----|------|-------------|
| `tools` | array | Custom tool definitions to add to the catalog. |
| `ignore` | array of strings | Names of built-in tools to remove from the catalog. |

Tool entry fields (all snake_case in JSON):

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier. If it matches a built-in, your entry overrides the built-in. |
| `description` | no | Shown in the TUI. |
| `source_image` | no | Docker image to multi-stage `COPY --from`. Required if any instruction uses `%s`. |
| `default_tag` | no | Tag for `source_image`, for example `3.13`. If `tag_suffix` is set, ccodolo appends the suffix automatically at render time — store the suffix-free version here. |
| `tag_suffix` | no | Suffix appended to `default_tag` and to user-supplied versions when pinning, for example `-slim`. |
| `instructions` | yes | List of Dockerfile lines (`RUN ...`, `COPY --from=%s ...`). Supports the placeholders documented in [Template Placeholders](#template-placeholders) below. |
| `dependencies` | no | Other tool names to install automatically when this tool is selected. |
| `path_entries` | no | Paths to prepend to `PATH` in the final image. |
| `env_vars` | no | Map of environment variables set in the final image. |
| `update_source` | no | The upstream registry the tool updater queries for new versions (`docker-hub`, `github-releases`, `npm`, `pypi`, `quay`). Leave unset to skip auto-updates for this tool. |
| `update_ref` | no | The identifier to query at `update_source`, for example an `owner/repo` for GitHub Releases or a package name for npm/PyPI. |

ccodolo parses a `category` field but always discards it. Every custom tool
appears under the `Custom` category in the TUI, including overrides of
built-in tools.

## Adding a tool

```json
{
  "tools": [
    {
      "name": "htop",
      "description": "Interactive process viewer",
      "instructions": [
        "RUN apt update && apt install -y --no-install-recommends htop && rm -rf /var/lib/apt/lists/*"
      ]
    }
  ]
}
```

## Overriding a built-in tool

A custom entry whose `name` matches a built-in tool **fully replaces** the
built-in. The example above pins Python to 3.11 by giving the same name as
the built-in `python` tool, with a different `default_tag` and matching
`instructions`. ccodolo prints an informational message to stderr
(`custom-tools.json: overriding built-in tool "python"`), so the override is
visible in the build output.

## Removing built-in tools

```json
{ "ignore": ["ruby", "php"] }
```

The named tools disappear from the TUI and from the catalog entirely. If you
ask for a removed tool with `--tools ruby`, ccodolo returns a clear "unknown
tool" error.

If you ignore a tool that another tool depends on, the build fails at
resolve time. For example, ignoring `nodejs` while keeping `yarn` produces:
`tool "yarn" depends on "nodejs": unknown tool "nodejs"`. Either also ignore
the dependent tool, or supply a custom replacement with the same name as
the ignored built-in.

## Pulling from an internal registry

The `internal-cli` example in the file format above shows the multi-stage
pattern:

- Set `source_image` to your private registry image.
- Set `default_tag` to a tag.
- Use `COPY --from=%s ...` in `instructions`.

ccodolo substitutes `%s` with `source_image:default_tag` at render time.

## Template Placeholders

Every string in `instructions` is rendered through Go's
[`text/template`](https://pkg.go.dev/text/template) package before the image
reference is substituted. This lets you parameterize your instructions with
the tool's resolved tag. The following placeholders are available:

| Placeholder | Expands to | Notes |
|-------------|------------|-------|
| `%s` | `source_image:tag` | Classic positional substitution, applied **after** the template pass. Only meaningful inside `COPY --from=%s ...`. Requires `source_image` to be set. |
| `{{.ImageRef}}` | `source_image:tag` | Same value as `%s`, but usable anywhere in a line, for example in shell interpolation. Requires `source_image` to be set. |
| `{{.Tag}}` | The full resolved tag, for example `3.13-slim` or `1.5.0` | Use when templating version numbers into `RUN` commands that install or download by version. |
| `{{.Version}}` | `{{.Tag}}` with `tag_suffix` stripped | Use when only the version number is needed, for example a path or package-name component. Identical to `{{.Tag}}` if `tag_suffix` is unset. |

The resolved tag is `default_tag` by default, or the user-supplied version
(passed via `--tools <name>:<version>` on the CLI, or the TUI version
picker), with `tag_suffix` appended when present. User overrides flow through
these placeholders automatically, so a tool whose `instructions` use
`{{.Tag}}` lets users pin any version without an edit to the catalog.

The `python` entry in the file format example above uses this pattern. Its
library path uses `{{.Version}}`. When you request `--tools python:3.12`,
ccodolo rewrites the `COPY` lines to `/usr/local/lib/python3.12` and points
`COPY --from=%s` at `python:3.12-slim`. One version change updates the whole
entry.

Another common pattern templates a CLI version into a `RUN` line, so users
can override it:

```json
{
  "name": "acme-cli",
  "description": "Acme internal CLI",
  "default_tag": "4.2.1",
  "instructions": [
    "RUN curl -fsSL https://downloads.acme.example/cli/v{{.Tag}}/acme-linux-amd64 -o /usr/local/bin/acme && chmod +x /usr/local/bin/acme"
  ]
}
```

Passing `--tools acme-cli:4.3.0` renders
`https://downloads.acme.example/cli/v4.3.0/...`, with no catalog change.

A malformed template produces a clear error at resolve time, for example
`tool "acme-cli": parsing instruction "...": template: instr:1: ...`.
ccodolo aborts the build. It never writes a broken Dockerfile.

## Constraints

Custom tool instructions can use only `RUN` and `COPY --from=image`. There
is no way to stage local files from your host into the build context. A
custom tool must fetch what it needs over the network, for example with
`curl`, `wget`, or `apt`. It can also pull the file from a Docker image
with `COPY --from=`.

If you need a build-time secret or a local file, use `build.custom_steps`
instead — see
[README → Custom Build Steps](../README.md#custom-build-steps).

## Failure modes

| Situation | Behavior |
|-----------|----------|
| File missing | Silent no-op. |
| File present but corrupt JSON | ccodolo prints a warning on stderr. It ignores the whole file. |
| File present but empty | ccodolo prints a warning on stderr (`file ... is empty`). It ignores the whole file. |
| Entry with empty `name` | ccodolo prints a warning. It skips that entry. The others still load. |
| Entry with empty `instructions` | ccodolo prints a warning. It skips that entry. The others still load. |
| Same `name` twice in `tools` | ccodolo prints a warning. The last definition wins. |
| `ignore` entry that matches no built-in | ccodolo prints a warning. It drops that ignore entry. |
| `ignore` removes a tool that another tool depends on | The dependent build fails at resolve time with a clear error. |
