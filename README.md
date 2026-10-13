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
for pages and `r` to refresh the table; Esc restores the template and page.

Press `L` in job-template details to open the launch form. Tab/Shift-Tab moves
through survey fields, the multiline `extra_vars` editor, and Launch/Cancel
buttons. Use arrows and Space/Enter for choice questions. Enter inserts a newline
in textarea and `extra_vars` fields; launch only by activating the Launch button.
Left/right, Home/End, Backspace/Delete, and up/down edit text; Ctrl-U clears the
focused input. Pasting is supported. Esc dismisses; Ctrl-C quits. Characters such
as `q` remain input while the form is open.

Surveys support text, textarea, password, integer, float, multiplechoice, and
multiselect questions, with defaults and required/length/numeric validation.
Password fields are masked; a saved password default remains on the controller
unless replaced. The manual editor accepts JSON or YAML, for example
`{"release": "v2", "debug": false}` or:

```yaml
release: v2
debug: false
```

Use Ctrl-U to replace the editor's initial `{}` with YAML. Input must be one
mapping with string keys and JSON-compatible values. Duplicate keys, YAML
aliases/merge keys/custom tags, and more than 64 KiB of launch input are rejected;
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
Job views separate **Capabilities** from **Job details**. Capabilities list output
(`o`), workflow children (`w`), cancellation (`c`), and relaunch (`R`) with their
availability and reasons. All displayed API-returned job information, including execution settings,
appears under Job details. Cancellation still requires confirmation.
Credential references show only IDs/names; credential contents, arbitrary
variables, module arguments, job environments, and artifacts are not displayed.
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
visible, and cancellation uses a centered prompt that takes keyboard focus and
identifies the job by name, ID, and type. Messages have a distinct panel. Help stays
at the bottom, with compact hints on narrow terminals. Output text remains plain;
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
synthetic HTTPS APIs for both modes, including inventory/group traversal, host
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
