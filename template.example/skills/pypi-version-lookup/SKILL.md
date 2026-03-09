---
name: pypi-version-lookup
description: Look up the latest version of any Python package on PyPI. Use this skill whenever you need to check a package's current version — for example, when writing or updating requirements.txt, pyproject.toml, setup.cfg, Dockerfiles, CI configs, or any dependency specification. Also trigger when the user asks "what's the latest version of X", "is there a newer version of X", or when pinning dependencies. This skill is fast, lightweight, and does not require pip or Python — it uses curl and jq directly.
allowed-tools: curl, jq
---

# PyPI Version Lookup

Look up the latest published version of a Python package from the PyPI JSON API using `curl` and `jq`.

## When to use this

- The user asks for the current/latest version of a Python package
- You need to pin or update a dependency version in any config file
- You're generating a requirements file and want accurate, up-to-date versions
- You want to verify whether a specific version exists

## How it works

Query the PyPI JSON API for a package and extract the `info.version` field, which always reflects the latest stable release.

```bash
curl -sf --max-time 5 "https://pypi.org/pypi/<package>/json" | jq -r '.info.version'
```

Replace `<package>` with the exact PyPI package name (e.g., `requests`, `numpy`, `flask`).

### Flags explained

- `-s` — Silent mode; suppresses progress output.
- `-f` — Fail silently on HTTP errors (returns a non-zero exit code instead of HTML error pages). This makes it easy to detect failures via `$?`.
- `--max-time 5` — Abort if the entire request takes longer than 5 seconds.

### Handling multiple packages

When looking up several packages at once, run the lookups in a loop:

```bash
for pkg in requests flask numpy; do
  version=$(curl -sf --max-time 5 "https://pypi.org/pypi/${pkg}/json" | jq -r '.info.version')
  if [ $? -eq 0 ] && [ -n "$version" ] && [ "$version" != "null" ]; then
    echo "${pkg}==${version}"
  else
    echo "${pkg}: lookup failed" >&2
  fi
done
```

## Error handling and retries

Network blips happen. If a lookup fails (non-zero exit code, empty result, or the string `"null"`), retry up to 3 times with exponential backoff before giving up:

```bash
lookup_version() {
  local pkg="$1"
  local attempt=0
  local max_retries=3
  local version=""

  while [ $attempt -lt $max_retries ]; do
    version=$(curl -sf --max-time 5 "https://pypi.org/pypi/${pkg}/json" | jq -r '.info.version')
    if [ $? -eq 0 ] && [ -n "$version" ] && [ "$version" != "null" ]; then
      echo "$version"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep $((2 ** attempt))  # 2s, 4s, 8s backoff
  done

  echo "Failed to fetch version for '${pkg}' after ${max_retries} attempts." >&2
  return 1
}
```

### Common failure causes

- **Package name typo** — PyPI names are case-insensitive but must otherwise match exactly. Some packages use hyphens on PyPI but underscores in import (e.g., `scikit-learn` on PyPI, `sklearn` in code). When unsure, try the hyphenated form first.
- **Network timeout** — The 5-second max-time handles this. The retry logic above will back off and try again.
- **Package doesn't exist** — curl will return a non-zero exit code thanks to `-f`. Check `$?` to detect this.

## Example usage

Single package:

```bash
curl -sf --max-time 5 "https://pypi.org/pypi/requests/json" | jq -r '.info.version'
# → 2.32.3
```

With retry wrapper:

```bash
version=$(lookup_version "requests")
echo "requests==${version}"
# → requests==2.32.3
```

## Extra info available from the API

The `.info` object has more than just `version`. Useful fields if needed:

- `.info.summary` — One-line package description
- `.info.requires_python` — Python version constraint (e.g., `>=3.8`)
- `.info.home_page` or `.info.project_urls` — Links to docs/repo
- `.info.license` — License string

Example — get version and Python requirement together:

```bash
curl -sf --max-time 5 "https://pypi.org/pypi/flask/json" | jq -r '"\(.info.version) (requires Python \(.info.requires_python))"'
# → 3.1.0 (requires Python >=3.9)
```
