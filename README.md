# AAP TUI

A keyboard-driven terminal client for Ansible Automation Platform 2.6. Browse
unified templates and recent jobs, inspect details, navigate workflow children,
scroll and follow output, and explicitly confirm supported job cancellation.

**AAP 2.6 compatibility has not been tested against a deployed instance.** The
implementation is verified offline with synthetic HTTPS servers and fixtures.
[API contracts and evidence](docs/API_CONTRACT.md) distinguish Red Hat product
documentation from assumptions based on pinned upstream AWX source.

## Install and run

Requires Go 1.25 or newer and an interactive terminal. Dependencies are pinned to
Bubble Tea v2.0.9, Lip Gloss v2.0.0, and ANSI layout helpers. Bubbles components
were not needed for these screens. Bubble Tea includes the upstream fix that
ensures clear-screen requests repaint even when the view has already been drawn.
The renderer dependency includes the fix for unsolicited debug-file creation.

```bash
go build -o aaptui ./cmd/aaptui
export AAP_URL=https://aap.example.com
read -r -s -p 'AAP token: ' AAP_TOKEN
export AAP_TOKEN
printf '\n'
./aaptui
```

Use an existing token with the permissions you need. Tokens are accepted only
through `AAP_TOKEN`; they are never written to configuration or cache metadata.

## Configuration

Copy [config.example.json](config.example.json) to
`$XDG_CONFIG_HOME/aaptui/config.json`, or `~/.config/aaptui/config.json` when XDG
configuration home is unset. Choose another file with `--config PATH`.
An absent default file is fine; an absent explicit file is an error.

| Setting | Environment override | Default |
| --- | --- | --- |
| `url` | `AAP_URL` | required |
| `connection_mode` | `AAP_CONNECTION_MODE` | `gateway` |
| `tls_verify` | `AAP_TLS_VERIFY` | `true` |

Precedence is defaults, file, then environment (including explicit false and empty
values). Empty required values, invalid modes/booleans, unknown JSON fields, token
fields, null values, and configuration files larger than 64 KiB are rejected.

`gateway` uses `/api/controller/v2/`; `direct` uses `/api/v2/`. Direct token
acceptance depends on the deployment. URLs must be HTTPS origins without paths,
credentials, queries, or fragments. System certificate trust is used. Setting
`AAP_TLS_VERIFY=false` disables verification and is shown in the connection banner.
HTTP, custom CA bundles, client certificates, and proxy path prefixes are outside
scope. Authenticated redirects and foreign API links are rejected.

### Authentication troubleshooting

`read page: authentication` means the server returned HTTP 401, so the list could
not be loaded. It does not mean there are no jobs or templates. Check that
`AAP_TOKEN` contains the raw access token (without a `Bearer ` prefix, surrounding
quotes, or spaces), and that it has not expired or been revoked.

For AAP 2.6, use a platform gateway OAuth 2 token with the gateway origin in
`AAP_URL` and `AAP_CONNECTION_MODE=gateway`. Red Hat documents
[gateway token authentication](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/develop-con_api_auth_methods)
and the [move of token authentication to gateway](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/upgrade-assembly_upgrade_api_changes).
Use `direct` only with a controller origin and a token that deployment accepts.
Restart aaptui after changing environment settings; they are loaded at startup.
HTTP 403 is a separate permission failure: check the token owner's access to the
requested resources. Do not share token values when reporting errors.

## Keys

The TUI uses the terminal's default background with a palette adapted to light
or dark terminals when background detection is available (otherwise it starts
with the dark palette). Selected rows use reverse video, bold text, and a `>`
marker. Job statuses retain their text: active is cyan, successful is green,
failed/error is red, and waiting states are amber. Selection and status labels
remain usable without color.

Lists align resource fields into columns and prioritize names and job statuses
on narrow terminals. Details wrap and scroll, search keeps the input cursor
visible, and messages and cancellation prompts have a distinct panel. Help stays
at the bottom, with compact hints on narrow terminals. Output text remains plain;
following shows the newest lines when the terminal shrinks. Extremely small
terminals show only the hints and content that fit; enlarge the terminal for
full details and messages.

| Screen | Keys |
| --- | --- |
| Everywhere | Ctrl-C quits; `q` quits outside search; Esc returns |
| Main/lists | arrows or `j`/`k` select; Enter opens |
| Template/job lists | `/` enters search; Enter applies server-side search; Esc exits input; `n`/`p` next/previous page; `r` refreshes |
| Template details | arrows or `j`/`k` scroll wrapped details; Esc returns |
| Job details | `o` output; `w` workflow children; `c` cancellation dialog; `r` refresh; arrows or `j`/`k` scroll |
| Workflow children | Enter opens a launched child; `n`/`p` page; `r` refresh |
| Output | arrows or `j`/`k` scroll; PgUp/PgDown page output; left/right or `h`/`l` pan; `f` follows newest output; `r` resumes after a stopped error |
| Cancellation dialog | `y` or Enter confirms the identified job; `n` or Esc dismisses |

Search consumes ordinary characters, including `q`. Search and workflow results
redraw the list with the footer anchored to the bottom of the terminal. Esc
repaints the restored screen to clear content from the screen being left. Pending
or skipped workflow nodes remain visible without opening a nonexistent child. Nested
workflows retain back-navigation state. Inaccessible and deleted resources display a safe error.
Unknown job/template types retain common information; unsupported capabilities
remain explicitly unknown or unavailable. Approval/denial is outside scope.

Only a confirmed cancellation performs a server mutation. State and token
permissions are rechecked immediately beforehand. An ambiguous cancellation
response triggers a status read, never an automatic repeat of the POST.
Quitting or leaving output cancels local work, not server jobs.

## Output and caching

Output polls every two seconds, with immediate bounded catch-up when behind.
Recoverable failures retain the cursor and retry with exponential backoff capped
at 30 seconds (rate-limit guidance may extend a wait to 60 seconds). Authentication
and permission failures stop automatic retries; `r` attempts to resume.
Scrolling pauses automatic scrolling while collection continues. Only one live
update and one current history request are pending; superseded history requests
are cancelled, and old-session results are ignored.

Ranged JSON stdout is decoded as raw text. ANSI/control sequences are removed and
the known token is redacted before display or storage. Overlapping last lines are
replaced to handle mutable partial lines. Final output is drained using terminal
status plus `event_processing_finished`; if that field is absent, the UI reports
unknown completion evidence and continues polling until you leave.
System jobs with inline stdout use bounded client-side line slicing.

Limits per output session:

- HTTP response: 1 MiB; output chunk/window: 256 lines and 256 KiB.
- Hot text cache: 256 KiB, accounting 64 bytes per cached line in addition to text.
- Disk cache: 16 MiB, at most 256 files and 65,536 indexed lines; oldest files evict
  first. Index metadata and temporary decoding buffers have separate finite bounds.
- Lists: 50 resources by default, at most 100 per page. Filtering stays server-side.

Caches live in random `~/.cache/aaptui/session-*` directories located using the
user's home directory. Directories have mode 0700 and files 0600. Symlink cache
ancestors are rejected. Cache files contain potentially sensitive job output,
with the known token redacted. Owned session files are removed on screen close
and app exit; storage and cleanup failures are reported.

Uncached older lines are fetched on demand without moving the live cursor
backwards. Expired history, gaps, resets, byte budgets, and server stdout limits
produce explicit errors. There is no download-only fallback for output exceeding
the server display limit. Inline system stdout above 256 KiB is rejected. A hard
kill or interrupted process can leave private session directories; inspect and
remove those specific leftovers yourself. The app does not sweep other sessions.

## Verification and limitations

```bash
go test ./...
go vet ./...
go test -race ./...
```

Tests exercise configuration, both API prefixes, TLS defaults, authentication,
unsafe links/redirects, every required resource type, workflow nodes, bounded
storage, older-history reload, retries, delayed final output, cleanup, and
confirmation/status reconciliation. Integration tests drive the TUI against
synthetic HTTPS APIs for both modes. Terminal interaction still needs manual
validation in the user's terminal, and deployed AAP 2.6 validation remains deferred.

Launching, relaunching, editing, approval/denial, and WebSocket transport are not
implemented. Server cancellation permissions and capability/state responses remain
authoritative. See [PLAN.md](PLAN.md) and [SPEC.md](SPEC.md) for scope.
