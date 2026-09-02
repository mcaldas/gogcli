# Fork setup (second machine)

**This file exists only in the `mcaldas/gogcli` fork.** It is not part of upstream
and must never be included in an upstream pull request — keeping it in a file
upstream does not have is also what keeps rebases conflict-free.

## What this fork adds

Branch `feat/mcp-send-tools-v037`, rebased on upstream `v0.37.1`.

| Tier | Tools | Gate |
| --- | --- | --- |
| read | 14 | on by default |
| write | 11 | `--allow-write` |
| send | 5 | `--allow-send` (this fork's addition) |

The send tier (`gmail_send`, `drive_upload`, `drive_mkdir`, `calendar_create`,
`sheets_clear`) is a third risk tier that `--allow-write` alone never exposes.
See [MCP server](mcp.md) for the full tool list and the config policy.

## 0. What you need

- **Go** (1.26+). `brew install go` on macOS.
- **macOS only:** any Apple-issued code-signing identity. See step 2 — this is
  not optional and not cosmetic.
- The OAuth client JSON from Google Cloud (step 4).

## 1. Clone and build

```bash
git clone https://github.com/mcaldas/gogcli.git ~/Documents/code/gogcli
cd ~/Documents/code/gogcli
git checkout feat/mcp-send-tools-v037

mkdir -p ~/.local/opt/gogcli-openclaw
go build -o ~/.local/opt/gogcli-openclaw/gog ./cmd/gog
```

Build **straight to the destination**. Do not build elsewhere and `cp` the
binary: on Apple Silicon, copying a Go binary invalidates its signature and
macOS kills the copy with `Killed: 9` (exit 137). If you must copy it, re-sign
afterwards.

## 2. Sign it (macOS — mandatory)

`gog` turns on macOS Keychain application trust **only when the running binary's
signature carries a TeamIdentifier** (`internal/secrets/keychain_trust.go` →
`codesignOutputHasStableIdentity`). Without one, the token item is created with
an empty trusted-application list — meaning *trust nobody* — so every single
read waits for an interactive click, and unattended callers (cron, a gateway,
Claude Desktop at launch) hang until `keyring connection timed out after 30s`.

`codesign` populates TeamIdentifier **only for certificates that chain to
Apple's developer roots**. A self-signed certificate reports
`TeamIdentifier=not set` no matter what you put in its subject — this was tried
and it does not work.

Pick any Apple identity you already have:

```bash
security find-identity -v -p codesigning
```

`Apple Development: …`, `Apple Distribution: …` and `Developer ID Application: …`
all carry a team ID and all work. A free Apple ID's "Apple Development"
certificate (created by Xcode with a personal team) is enough. Then:

```bash
IDENTITY="Apple Development: Your Name (TEAMID1234)"   # or the SHA-1 hash

codesign --force --sign "$IDENTITY" \
  --identifier com.steipete.gogcli.gog --timestamp \
  ~/.local/opt/gogcli-openclaw/gog

codesign --verify --strict ~/.local/opt/gogcli-openclaw/gog
codesign -dv ~/.local/opt/gogcli-openclaw/gog 2>&1 | grep TeamIdentifier
```

The last line must print a real team ID, not `TeamIdentifier=not set`.

The resulting designated requirement pins the certificate's **common name, not
its hash**, so the signature survives both rebuilds and the annual certificate
renewal. You do not need the same identity as any other machine — each machine's
keychain items are created by that machine's own binary.

**No Apple identity available?** `GOG_KEYCHAIN_TRUST_APPLICATION=true` forces
trust on regardless of signature. It works, but an ad-hoc or unsigned binary
has a hash-pinned requirement, so the grant dies on every rebuild and you are
back to clicking. Treat it as a stopgap.

## 3. Put it on PATH — and keep exactly one `gog`

```bash
ln -sf ~/.local/opt/gogcli-openclaw/gog /opt/homebrew/bin/gog
gog --version
```

**Do not also install `brew install openclaw/tap/gogcli`.** A keychain item's
trusted-application list names one signing identity; Homebrew's binary and this
one are signed by different teams and can never both be in it. Two binaries
means whichever one runs second gets prompted forever. If Homebrew's is already
installed, `brew uninstall gogcli` first.

Anything that finds `gog` through `PATH` — agents, cron jobs, a gateway — will
pick up whatever is in `/opt/homebrew/bin`, so the symlink is what actually
routes those callers to the fork.

## 4. OAuth client credentials

There is no bundled OAuth client. Every machine needs one at
`~/Library/Application Support/gogcli/credentials.json` (macOS) or
`$XDG_CONFIG_HOME/gogcli/credentials.json` (Linux).

Either copy the file from the first machine, or create a fresh one:
Google Cloud console → APIs & Services → Credentials → Create Credentials →
OAuth client ID → **Desktop app** → download the JSON, then:

```bash
gog auth credentials ~/Downloads/client_secret_xxx.json
gog auth credentials list
```

**A refresh token is bound to the client that issued it.** If you plan to
import tokens from the first machine (step 5a), you must copy that machine's
`credentials.json` — a new OAuth client will reject them.

## 5. Add your accounts

### 5a. Import from the first machine (no browser needed)

On the machine that already works:

```bash
gog auth tokens export you@example.com --out /tmp/you.token --overwrite
```

Copy the file across (it contains a live secret — use `scp`, delete it after),
then on the new machine:

```bash
gog auth tokens import /tmp/you.token
```

### 5b. Fresh consent (browser)

```bash
gog auth add you@example.com \
  --services gmail,calendar,drive,docs,sheets \
  --drive-scope full --gmail-scope full \
  --force-consent --timeout 15m
```

Notes that cost real time:

- **Docs and Sheets need their own service entries.** `drive` alone does not
  enable `docs_create` or `sheets_*`.
- `--drive-scope` accepts `full|readonly|file` and `--gmail-scope` accepts
  `full|readonly|send|read-send`, so a narrower grant is possible if you want
  one. For an account the MCP server serves with `--allow-send`, **`read-send`
  is the better choice than `full`** — it grants reading and sending without
  full mailbox modify rights.
- **A narrowed Gmail or Drive scope disables incremental authorization.** With
  `--gmail-scope` set to anything but `full`, `auth add` stops merging
  previously granted scopes, so name every service you want in that one
  command. `--readonly` cannot be combined with `send` or `read-send`.
- **Run one flow at a time.** Concurrent `auth add` runs collide on the
  `127.0.0.1` callback port.
- **Sign in to the right Google account in the browser first.** Consenting as
  the wrong one prints `authorized as X, expected Y` and discards the result.
- A timed-out consent **exits 0**. The exit code lies; check the output.
- `auth add` *replaces* rather than merges. Export a backup before re-consenting
  an account that already works.

## 6. Verify

```bash
gog auth doctor
```

`keychain.trust` must read `application trust enabled (developer-id signed)`.

Then prove the unattended path — this is the one that used to hang:

```bash
env -i PATH=/usr/bin:/bin HOME="$HOME" \
  /opt/homebrew/bin/gog calendar list --json | head -5
```

It should return in about a second with no prompt. If it hangs,
`GOG_KEYRING_OPEN_TIMEOUT` takes a Go duration string (`5m`) to give yourself
time to click while debugging.

`gog auth list` **under-reports scopes right after a grant** — the stored record
shows only the newly requested scopes until a token refresh, then jumps to the
union. Force convergence with a real API call, not with `auth doctor` alone.

## 7. Wire into Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json`, one server
per account:

```json
{
  "mcpServers": {
    "gog-personal": {
      "command": "/Users/YOU/.local/opt/gogcli-openclaw/gog",
      "args": ["--account", "you@example.com", "mcp", "--allow-write", "--allow-send"]
    }
  }
}
```

Point `command` at the real binary, not the symlink. Quit Claude Desktop
entirely (⌘Q) and reopen — reloading the window is not enough.

Drop `--allow-send` for a read+write server, or both flags for read-only.
Consider leaving `--allow-send` off for an account whose inbox the model also
reads: reading untrusted mail *and* being able to send from the same account is
the full prompt-injection exposure the upstream design deliberately avoids.

## 8. Linux and other platforms

Steps 2 and 3 are macOS-only — there is no codesign and no keychain trust.
Build, put the binary on `PATH`, and pick a keyring backend:

```bash
GOG_KEYRING_BACKEND=file GOG_KEYRING_PASSWORD=... gog auth doctor
```

The `file` backend is also the right choice for headless servers and
containers. See [Install](install.md) for the container recipe.

## 9. Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `keyring connection timed out after 30s` | Binary has no TeamIdentifier, or the item was created by a differently-signed binary | Step 2, then recreate the items (step 10) |
| A prompt on every launch, even after "Always Allow" | Ad-hoc signature: the requirement is hash-pinned and dies on each rebuild | Sign with an Apple identity |
| `Killed: 9` / exit 137 | Signature invalidated by copying the binary | Build straight to the destination, or re-sign |
| MCP server shows no tools in the client | Flags missing, or the client was not fully restarted | Check `args`, then ⌘Q and reopen |
| `gmail_send` missing but `docs_write` present | `--allow-write` without `--allow-send` | Working as designed — add the flag |
| Imported token rejected | Different OAuth client than the one that issued it | Copy the source machine's `credentials.json` |

## 10. Repairing a bad keychain item

The trusted-application list is written **at item creation only** — the keyring
library skips it on updates on purpose, so an ordinary token refresh never
repairs a bad item. It has to be deleted and recreated by the signed binary:

```bash
gog auth tokens export you@example.com --out ~/you.token.bak --overwrite
gog -y auth tokens delete you@example.com
gog auth tokens import ~/you.token.bak
```

`auth tokens delete` **needs `-y`** non-interactively, or it silently no-ops and
the following import merely updates the old item, leaving the bad list in place.
The export is your only backup — never delete an item whose export failed.

## 11. Keeping up with upstream

```bash
git fetch origin
git rebase origin/main            # tag a backup first
make ci
go build -o ~/.local/opt/gogcli-openclaw/gog ./cmd/gog   # then re-sign, step 2
```

Conflicts land almost entirely in `internal/cmd/mcp.go` and
`internal/cmd/mcp_tools.go`, upstream's fastest-moving MCP files. Resolve
`internal/cmd/mcp_test.go` by hand — a keep-both merge silently swallows Go
closing braces.

Re-sign after **every** rebuild (step 2). An unsigned rebuild reintroduces the
prompting immediately.
