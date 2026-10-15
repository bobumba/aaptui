# AAP TUI

A keyboard-driven terminal client for Ansible Automation Platform 2.6. Browse
job templates, projects, inventories, and recent jobs of all types, inspect details, navigate
workflow children, scroll and follow output, and explicitly confirm supported
job cancellation and relaunch, or launch job templates with survey answers and
manual variables.

The header displays the effective AAP URL, the username resolved through `me/`,
and the number of jobs with status `running` visible to the authenticated token
across job types. Header requests run asynchronously and do not interrupt
navigation. The running count refreshes every five seconds using the API's
filtered total, without downloading all jobs. Temporary failures retry with
backoff capped at 30 seconds (rate-limit guidance may extend a wait to 60 seconds).
Failed refreshes mark the last count as stale, or unavailable before the first
successful fetch. Authentication, permission, and malformed-response failures
stop count polling. Restart to retry these failures or identity lookup failures.
Quitting stops header requests and polling waits.

The main-menu Jobs list includes start and finish dates and times in UTC. Wide
terminals show timestamp columns; narrower terminals use wrapped timestamp rows.
Missing timestamps show `—`; invalid timestamps show `Unavailable`.

Job-template details also show a table of that template's jobs, newest first,
with ID, name, status, and start time (columns adapt to terminal width). Tab
switches focus between details and the table; arrows scroll details or select
a job. Enter opens job details and their existing output controls. Use `n`/`p`
for pages and `r` to refresh the focused details or table; Esc restores the
template and page.

Press `L` in job-template details to open a centered launch dialog styled like
the cancel/relaunch prompts. Each survey field and the multiline `extra_vars`
editor has a separate box; the focused box uses a double border and a `>` marker.
The form scrolls to keep the focused input and cursor visible as you move or
resize the terminal. Validation feedback identifies the field and also appears
inside its box when space permits. Short or narrow terminals simplify borders
and help; enlarge the terminal for full labels, descriptions, and controls.
Tab/Shift-Tab moves through survey fields, the multiline `extra_vars` editor,
and Launch/Cancel buttons. Use arrows and Space/Enter for choice questions. Enter inserts a newline
in textarea and `extra_vars` fields; launch only by activating the Launch button.
Left/right, Home/End, Backspace/Delete, and up/down edit text; Ctrl-U clears the
focused input. Pasting is supported. Esc dismisses; Ctrl-C quits. Characters such
as `q` remain input while the form is open.

Surveys support text, textarea, password, integer, float, multiplechoice, and
multiselect questions, with defaults and required/length/numeric validation.
Password fields are masked; a saved password default remains on the controller
unless replaced. The manual editor starts empty and accepts JSON or YAML, for
example `{"release": "v2", "debug": false}` or:

```yaml
release: v2
debug: false
```

Type or paste either format directly; blank or whitespace-only input adds no
manual overrides. Input must be one mapping with string keys and JSON-compatible
values. Duplicate keys, YAML aliases/merge keys/custom tags, and more than 64 KiB
of launch input are rejected;
quote dates to pass them as strings. Define survey variables in their survey
controls; matching manual keys are rejected. Arbitrary manual variables require
**Prompt on launch** for Extra Variables. Blank optional numeric/choice/password
answers are omitted; blank optional text is submitted only when minimum length
is zero. The controller applies template variables, then valid survey defaults,
then submitted runtime values.

Launching requires start permission and a valid exposed launch endpoint. Missing
permission is shown as unknown. Templates requiring inventory, credentials, or
credential passwords that the form cannot supply explain that the controller UI
is required. Permissions and survey definitions are checked again before launch.
Inputs remain in memory and are cleared when leaving the form. Success opens the
created job (or a workflow job for sliced templates); Esc restores the template.
The new identity remains available if details fail to load. Warnings report
ignored inputs without displaying their values. Uncertain launch outcomes disable
resubmission; close the form and refresh Jobs before trying again. Leaving during
submission cancels the local request but might still create a server job.

The main menu contains Job Templates, Jobs, Projects, and Inventories. Inventories
lists the `inventories/` collection with search, refresh, and pagination.
Selecting an inventory opens Groups and Hosts. Groups lists all inventory groups;
selecting a group opens Child groups and Hosts. Group host lists include hosts
from descendant groups. Use `n`/`p` to reach hosts and groups across pages; Esc restores the previous
page, search, selection, and inventory/group context. Inventory and group screens
also display object details; Tab switches between menu selection and detail
scrolling. Enter on a host opens its details, including enabled state, owning
inventory, instance ID, failure flags, last job, and facts timestamp. The Groups
section lists visible direct and inherited memberships, including parent groups;
use `n`/`p` to page memberships and arrows or `j`/`k` to scroll. Loading, empty,
unavailable, and failed membership requests have distinct states; other host
information remains readable. Use `r` to refresh inventory, group, or host details
and host memberships.

Object views include pertinent fields reviewed against the pinned API source:
creation/modification times and ownership, job launch and failure information,
execution settings and related resource IDs/names, template run history, SCM
revision and update settings, and inventory counts and failures. Available
boolean and numeric fields preserve false and zero. Missing timestamps show `—`;
optional fields not returned by the server are omitted. Details use aligned
label/value tables with bold, theme-aware colored labels. Wide terminals show
two field/value pairs per row; narrower terminals use one pair per row, with a
stacked layout at very small widths. Descriptions and multiline values span the
full table width. Long labels and values wrap, and all detail rows can be scrolled.
Job views show **Capabilities** and **Job details**, with recorded variables in
an **extra_vars** row rendered as YAML without a border. The YAML spans the full
table width and preserves indentation and spaces when wrapping. Job-template
details show configured variables in an **Extra variables** box with theme-aware
styling. The box wraps within the terminal width and scrolls with other details;
extremely narrow terminals omit its border to leave room for content.
Capabilities list output
(`o`), workflow children (`w`), cancellation (`c`), and relaunch (`R`) with their
availability and reasons. Execution settings and other job fields appear under
Job details. Cancellation still requires confirmation.

The extra_vars row shows the values recorded for playbook jobs,
workflow jobs, ad hoc commands, and system jobs. Survey answers and other variables
appear together, sorted by variable name; current template/survey definitions are not
used to infer their origins. Nested mappings and lists use indented YAML, and
multiline answers use YAML block scalars where possible. Empty strings show `""`;
false, zero, null, and precise numbers remain visible. Job-template variables
retain formatted JSON for nested values and line breaks for multiline answers,
showing the template's configured values with the same limits. Use arrows or
`j`/`k` to scroll details and `r` to refresh them (focus
details with Tab when the template's job table is shown). Empty, unavailable,
malformed, oversized, and unsupported data have distinct messages; other job fields remain
readable. Server password masks are retained and the configured token is redacted
from names and values. Variable data stays in memory, with 64 KiB decoded-input
and display limits and at most 64 nesting levels.

Credential references show only IDs/names; credential contents, variables outside
jobs and job templates, module arguments, job environments, and artifacts are not
displayed.
SCM URLs omit embedded credentials, query parameters, and fragments.

On workflow children, `d` opens node details, including template/child references,
convergence and skip flags, and success/failure/always node IDs. Pending and
skipped nodes remain inspectable. Enter still opens a launched child job.

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
Terminal output uses the widely supported column-position command so older
terminal multiplexers reporting `TERM=screen` render list columns correctly.

```bash
go build -o aaptui ./cmd/aaptui
./aaptui
```

Every launch lists the saved AAP URLs and offers **Create new configuration**.
Use arrows or Tab to select and Enter to continue. Selecting a URL uses that
connection's mode, TLS setting, and token, prompting for credentials if needed.
The list appears even with one saved URL. Invalid connections are unavailable;
new setup remains accessible when the list is empty or a file cannot be read.
Long lists scroll to keep the selected URL visible.

For a new connection, enter the host URL, connection mode (`gateway` or `direct`),
TLS verification (`true` by default), username, and password. Password entry is masked. Submitting
your credentials requests a personal access token with write scope, subject to
your existing permissions. After successful authentication, the app saves the
returned token, URL, mode, and TLS setting in `~/.config/aaptui/config.json`.
It never saves the login username or password. Gateway mode requests a token at
`/api/gateway/v1/tokens/`; direct mode uses `/api/v2/tokens/`. Endpoint availability
and permission to create tokens depend on the deployment. Esc, Ctrl-C,
and Ctrl-D cancel either prompt; losing terminal input also cancels it.

You can still use an existing token:

```bash
export AAP_URL=https://aap.example.com
read -r -s -p 'AAP token: ' AAP_TOKEN
export AAP_TOKEN
printf '\n'
./aaptui
```

Select the URL supplied by `AAP_URL` to use its effective file/environment
settings. This entry appears alongside saved URLs; overrides apply only to that
origin. Complete supplied URL/token pairs connect without further setup or
writing a credential file. Supply `AAP_URL` with `AAP_TOKEN`; without a URL,
select **Create new configuration**. New setup always requests a token using
your entered username/password and uses your entered settings for this launch.
Startup requires an interactive terminal; redirected input/output fails
immediately rather than waiting for selection. Tokens are never accepted as
arguments or written to job-output cache metadata.

## Configuration

Copy [config.example.json](config.example.json) to
`$XDG_CONFIG_HOME/aaptui/config.json`, or `~/.config/aaptui/config.json` when XDG
configuration home is unset. Choose another file with `--config PATH`.
An absent default file is fine; an absent explicit file or a malformed file
makes saved-URL selection unavailable while new setup remains accessible.
Saved credentials always stay in `~/.config/aaptui/config.json`, even when XDG
or `--config` selects another file. Other files cannot contain tokens.

The home file stores a `connections` array. Each entry contains `url`,
`connection_mode`, `tls_verify`, and an optional `token` saved after authentication.
For example, a settings file without credentials can contain:

```json
{
  "connections": [
    {"url": "https://aap.example.com", "connection_mode": "gateway", "tls_verify": true},
    {"url": "https://controller.example.com", "connection_mode": "direct", "tls_verify": true}
  ]
}
```

Existing single-connection files with top-level `url`/`token` remain readable.
They migrate into the array on the next successful credential save. Saving a
new origin preserves the existing connections. Saving the same origin updates
that entry, treating host case, default ports, and trailing slashes as equivalent.
The most recently saved connection appears first by default. Duplicate origins
within one file and mixing top-level URL/token fields with an array are rejected.
Optional top-level `connection_mode` and `tls_verify` act as defaults for entries
that omit those fields and are preserved when saving.

| Setting | Environment override | Default |
| --- | --- | --- |
| `url` | `AAP_URL` | no URL entry if missing |
| `token` (private home file only) | `AAP_TOKEN` | generate if missing |
| `connection_mode` | `AAP_CONNECTION_MODE` | `gateway` |
| `tls_verify` | `AAP_TLS_VERIFY` | `true` |

The first connection in a selected settings file supplies the effective URL;
otherwise the first home connection does. `AAP_URL` overrides that effective URL.
For that origin, use its saved settings, then selected-file settings, then
environment overrides (including explicit false and empty values). Only its
entry receives these overrides, and it appears first. Other saved URLs keep their
own settings and tokens. Additional URLs in a selected file are also selectable;
their explicit settings override matching saved settings. Saved tokens are reused
only for the same API origin, and `AAP_TOKEN` applies only to the effective entry.
If its origin is not saved, it starts from defaults plus selected-file/environment
settings and prompts for credentials unless `AAP_TOKEN` is supplied.

Invalid nonempty settings/tokens make their connection unavailable. Unknown JSON
fields, token fields outside the private home file, null values, blank URLs in
array entries, and files larger than 64 KiB are rejected. Fix unreadable or
malformed files before selecting saved connections.

The home credential directory must have permissions `0700` and its regular
configuration file `0600`; symlinked credential paths are rejected. Startup saves
atomically and preserves other settings files. New setup ignores `--config`, XDG
settings, and `AAP_*` values for the active connection. Cancellation,
authentication failures, and replacement-write failures preserve saved
connections. A malformed or symlinked home file is not overwritten; correct it
before saving a new connection. If storage has insecure permissions, correct them:

```bash
chmod 700 ~/.config/aaptui
chmod 600 ~/.config/aaptui/config.json
```

To replace an expired/revoked saved token, select **Create new configuration**
and enter the same server URL and fresh credentials. To sign in again using a
saved entry, remove its `token` field, unset `AAP_TOKEN`, and select that URL.
Removing an array entry forgets that connection; removing the file forgets all
connections and preferences. Local removal does not revoke server tokens;
revoke unwanted tokens in the server's token management UI.

`gateway` uses `/api/controller/v2/`; `direct` uses `/api/v2/`. Direct token
acceptance depends on the deployment. URLs must be HTTP or HTTPS origins without
paths, credentials, queries, or fragments. For example, set
`AAP_URL=http://localhost:8080` to connect over HTTP. HTTPS uses system certificate
trust; `AAP_TLS_VERIFY=false` disables certificate verification and is shown in
the connection banner. TLS settings do not apply to HTTP connections, which the
banner identifies as HTTP. Custom CA bundles, client certificates, and proxy path
prefixes are outside scope. Authenticated redirects and foreign API links are rejected.

### Authentication troubleshooting

`read page: authentication` means the server returned HTTP 401, so the list could
not be loaded. It does not mean there are no jobs, templates, or projects. Check
that `AAP_TOKEN` contains the raw access token (without a `Bearer ` prefix, surrounding
quotes, or spaces), and that it has not expired or been revoked.

For AAP 2.6, use a platform gateway OAuth 2 token with the gateway origin in
`AAP_URL` and `AAP_CONNECTION_MODE=gateway`. Red Hat documents
[gateway token authentication](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/secure-con_controller_api_oauth2_token)
and the [move of token authentication to gateway](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/upgrade-assembly_upgrade_api_changes).
Use `direct` with a controller origin exposing the controller API. It supports
username/password token creation at `/api/v2/tokens/` as documented by
[AWX API authentication](https://docs.ansible.com/projects/awx/en/24.6.1/rest_api/authentication.html).
Gateway is preferred for AAP 2.6; the migration guide marks legacy controller
token authentication deprecated and retains it for backward compatibility.
Direct endpoint availability remains deployment-dependent. If login fails,
Basic authentication or external-user token creation may be disabled; supply an
existing token through `AAP_TOKEN` and
select the matching URL, or contact the administrator.
Token creation is attempted once. After an uncertain response or a storage failure,
check server tokens before trying again, as a token may already have been created.
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
visible, and cancellation/relaunch use centered prompts that take keyboard focus
and identify the job by name, ID, and type. The launch dialog uses separate input
boxes and keeps Launch/Cancel controls visible when space permits. Messages have
a distinct panel. Help stays at the bottom, with compact hints on narrow
terminals. Output text remains plain;
following shows the newest lines when the terminal shrinks. Extremely small
terminals show only the hints and content that fit; enlarge the terminal for
full details and messages.

| Screen | Keys |
| --- | --- |
| Everywhere | Ctrl-C quits; `q` quits outside text input; Esc returns |
| Main/lists | arrows or `j`/`k` select; Enter opens |
| Template/project/job/inventory/group/host lists | `/` enters search; Enter applies server-side search; Esc exits input; `n`/`p` next/previous page; `r` refreshes |
| Inventory/group details and menus | Tab switches menu/details focus; arrows or `j`/`k` select/scroll; Enter opens the selected menu item; `r` refreshes details; Esc returns |
| Host lists/details | Enter opens details; arrows or `j`/`k` scroll details; `n`/`p` page direct/inherited groups; `r` refreshes details and memberships; Esc restores the list |
| Template details with jobs | `L` opens launch; Tab switches details/jobs focus; arrows or `j`/`k` scroll/select; Enter opens a job; `n`/`p` page; `r` refreshes jobs; Esc returns |
| Job-template launch | Tab/Shift-Tab changes field/button; Enter edits multiline input or activates a button; arrows/Space select choices; Ctrl-U clears input; Esc dismisses; Ctrl-C quits |
| Project details | arrows or `j`/`k` scroll wrapped details; Esc returns |
| Job details | `o` output; `w` workflow children; `c` cancellation dialog; `R` relaunch dialog; `r` refresh; arrows or `j`/`k` scroll |
| Workflow children | `d` opens node details; Enter opens a launched child; `n`/`p` page; `r` refresh |
| Workflow node details | arrows or `j`/`k` scroll; Enter opens a launched child; Esc restores the node list |
| Output | arrows or `j`/`k` scroll; PgUp/PgDown or Ctrl-U/Ctrl-D page output; left/right or `h`/`l` pan; `f` follows newest output; `r` resumes after a stopped error |
| Cancellation/relaunch dialog | `y` or Enter confirms the identified job; `n` or Esc dismisses; `q` or Ctrl-C quits |

Search consumes ordinary characters, including `q`. Search and workflow results
redraw the list with the footer anchored to the bottom of the terminal. Esc
repaints the restored screen to clear content from the screen being left. Pending
or skipped workflow nodes remain visible without opening a nonexistent child. Nested
workflows retain back-navigation state. Inaccessible and deleted resources display a safe error.
Unknown job/template types retain common information; unsupported capabilities
remain explicitly unknown or unavailable. Approval/denial is outside scope.

Only confirmed cancellation/relaunch or explicit launch-form submission performs
a server mutation. State and token
permissions are rechecked immediately beforehand. An ambiguous cancellation
response triggers a status read, never an automatic repeat of the POST.
While cancellation is pending, other job actions and refresh are blocked;
Esc and quit remain available. Progress, success, and failure are visible.

`R` relaunches supported playbook jobs, workflow jobs, and ad hoc commands using
their original settings and job limit. The confirmation identifies the source
job and explains that it creates a new execution. Success opens the new job's
details; Esc returns to the source job. If those details cannot be loaded, the
new job's identity remains available for `r` refresh. Required credential
passwords are not collected; those jobs show a reason to use the controller UI.
Missing relaunch permission information is shown as unknown. Project/inventory
updates, system jobs, approvals, and unknown types have no supported relaunch
operation. Other actions and refresh are blocked while relaunch is pending.
Uncertain relaunch responses are never automatically retried; refresh Jobs and
check for a new execution before retrying manually.

Quitting or leaving output cancels local work, not server jobs.

## Output and caching

Press `o` in job details to open the latest screenful of output. Lines remain in
chronological order; Page Up/Ctrl-U or up/`k` loads earlier output on demand.
Opening output does not download or replay the entire job log.

Output polls every two seconds for the latest bounded window, skipping intervening
history when output grows rapidly. `f` returns directly to the newest lines.
Recoverable failures retain the cursor and retry with exponential backoff capped
at 30 seconds (rate-limit guidance may extend a wait to 60 seconds). Authentication
and permission failures stop automatic retries; `r` attempts to resume.
Scrolling pauses automatic scrolling while collection continues. Only one live
update and one current history request are pending; superseded history requests
are cancelled, and old-session results are ignored.

Ranged JSON stdout is decoded as raw text. ANSI/control sequences are removed and
the known token is redacted before display or storage. Overlapping last lines are
replaced to handle mutable partial lines; a partial line skipped by a live jump
is fetched again when requested as history. The final latest window is fetched
using terminal status plus `event_processing_finished`; if that field is absent,
the UI reports unknown completion evidence and continues polling until you leave.
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
backwards. Unloaded ranges are allowed in the cache. Expired history, unexpected
ranges, resets, byte budgets, and server stdout limits produce explicit errors.
There is no download-only fallback for output exceeding
the server display limit. Inline system stdout above 256 KiB is rejected. A hard
kill or interrupted process can leave private session directories; inspect and
remove those specific leftovers yourself. The app does not sweep other sessions.

Tail requests bound client transfer, caching, and rendering work. The reviewed
upstream server still reads the full stdout to select a range, and its display
limit still applies. Actual AAP 2.6 tail behavior remains unverified.

## Verification and limitations

```bash
go test ./...
go vet ./...
go test -race ./...
```

Tests exercise configuration, both API prefixes, TLS defaults, authentication,
unsafe links/redirects, every required resource type, workflow nodes, bounded
storage, older-history reload, retries, delayed final output, cleanup, and
confirmation/status reconciliation. Startup regressions cover missing settings,
token creation in both connection modes, safe failures without retries, credential permissions,
atomic replacement, environment overrides, and server scoping. Startup-choice
tests cover complete/incomplete/invalid current settings, fresh setup despite
file/environment overrides, credential-driven token creation and storage, successful
replacement, and preservation after failures. Linux terminal tests check both
startup choices, password masking, graceful cancellation, and terminal
restoration; input EOF is covered for each prompt. Integration tests drive the
TUI against synthetic HTTPS APIs for both modes, including inventory/group traversal, host
pagination, identity/count header initialization, template-job paging, and
survey/manual-variable launch with restoration of the source template. Launch
tests cover variable validation/precision, password defaults/redaction, keyboard
focus, duplicate submission, changed requirements, ignored inputs, uncertain
outcomes, stale responses, and narrow terminals. Header polling tests use
deterministic timing to verify backoff and cancellation. Terminal interaction still needs manual
validation in the user's terminal. A Linux pseudo-terminal regression emulates
older `TERM=screen` sessions at 237 columns by 62 rows, checks the initial list
columns, and selects and opens the last template. Deployed AAP 2.6 validation
remains deferred.

Workflow-template launching, editing, approval/denial, and WebSocket transport are
not implemented. Server launch/cancellation/relaunch permissions and capability responses remain
authoritative. See [PLAN.md](PLAN.md) and [SPEC.md](SPEC.md) for scope.
