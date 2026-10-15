# AAP TUI Implementation Plan

Status: both milestones below are complete with offline verification. Deployed
AAP 2.6 and manual interactive-terminal validation remain unavailable.

This plan refines `SPEC.md` and follows `AGENTS.md`. No application code is
authorized by the design discussion alone. Implement one milestone at a time
when implementation is requested.

## Scope and decisions

- Support AAP 2.6 through the platform gateway or directly through controller.
- Use Bubble Tea v2, Bubbles components as needed, and Lip Gloss for styling.
  Pin compatible stable dependency versions during bootstrap.
- Use REST polling for live output, initially every two seconds. No WebSocket
  transport is planned.
- Include playbook jobs, workflow jobs, project updates, inventory updates,
  ad hoc commands, system jobs, and workflow approvals where the API exposes
  them. Preserve common details for unknown job types instead of hiding them.
- List only job templates through `job_templates/` in the Job Templates menu.
  List projects through `projects/` in the Projects menu, reusing the existing
  project summary and detail types. Retain type-aware detail readers for other
  template resources.
- Support template listing, search/filtering, and details, and retain relaunching
  of existing jobs where supported by the API and the current token's permissions.
  Add job-template launching with survey inputs and manually entered `extra_vars`.
  Workflow-template launching, approval/denial, and resource editing remain out of scope.
- Allow cancellation only for supported active jobs and the current token's
  permissions, following an explicit confirmation in the TUI.
- Keep output memory bounded. Fetch older output ranges on demand and use
  temporary cache files under the user's home directory when useful.
- Support HTTP and HTTPS origins. Do not add private CA configuration, client
  certificates, or reverse-proxy path prefixes. For HTTPS, use system certificate
  trust; retain the specified TLS-verification setting, enabled by default.
  TLS verification does not apply to HTTP connections.
- No AAP instance will be available. Offline tests and source review are the
  available verification methods; actual AAP 2.6 compatibility remains unverified.

## Project layout

```text
cmd/aaptui/main.go
internal/config/
    config.go
    config_test.go
internal/aap/
    client.go
    errors.go
    types.go
    templates.go
    jobs.go
    workflows.go
    output.go
    output_store.go
    *_test.go
    testdata/
internal/tui/
    model.go
    templates.go
    jobs.go
    details.go
    workflow.go
    output.go
    keys.go
    *_test.go
AGENTS.md
SPEC.md
PLAN.md
README.md
go.mod
go.sum
```

Keep packages focused and add files as needed by milestones. Do not introduce
separate service, repository, or general-purpose utility layers.

## Package responsibilities

`cmd/aaptui` loads and validates configuration, constructs the HTTP client and
TUI dependencies, handles process signals, and ensures cleanup and terminal
restoration. Dependency injection is explicit; there is no global client.

`internal/config` loads connection settings and secrets, including `AAP_TOKEN`
and the private credential file, and owns credential persistence.
It does not perform network requests or manage terminal state.

`internal/aap` owns REST paths, authentication, TLS, pagination, JSON decoding,
job-type dispatch, capability discovery, output transport, retries, cursors,
and output caching. It exposes Go resource types and operations, with no
Bubble Tea dependencies. HTTP response structs and server links stay private.

`internal/tui` owns navigation, selections, search input, layout, scrolling,
follow mode, loading/error state, and confirmation dialogs. Blocking operations
run in asynchronous commands and return typed result messages. Only the update
loop mutates TUI state. Consumer-defined interfaces allow tests with fakes.

## Configuration

Every launch lists saved AAP URLs alongside "Create new configuration".
Selecting a URL uses its own mode, TLS setting, and origin-bound token, prompting
for credentials when needed. Store connections in a JSON `connections` array.
Read legacy single-connection files and migrate them on the next successful
save. Add distinct origins; update matching origins without losing other entries.

List the effective selected-file/environment URL first and apply its overrides
only to that entry. Other saved URLs retain their own settings and credentials.
Use the first selected-file URL, or first home URL, unless `AAP_URL` overrides it.
Default mode/TLS settings may remain at the top level; reject mixed legacy
URL/token plus array formats and duplicate origins within one file.

New setup prompts for URL, connection mode, TLS verification, username, and masked
password, starting with gateway mode and TLS verification enabled. It requests
one token at the selected mode's endpoint and saves only after authentication.
New setup uses entered values for this launch, ignoring selected-file/environment
settings. An interactive terminal is required.

Use JSON and the standard library. The default file is
`$XDG_CONFIG_HOME/aaptui/config.json`, falling back to
`~/.config/aaptui/config.json`. Accept `--config PATH` to choose a different file.
Startup setup saves the API URL and token together in
`~/.config/aaptui/config.json`, resolved using `os.UserHomeDir`, regardless of
`XDG_CONFIG_HOME` or `--config`.

| Setting | JSON field | Environment variable | Default |
| --- | --- | --- | --- |
| Server URL | `url` per connection | `AAP_URL` | No entry if missing |
| Connection mode | `connection_mode` | `AAP_CONNECTION_MODE` | `gateway` |
| TLS verification | `tls_verify` | `AAP_TLS_VERIFY` | `true` |
| Authentication token | `token` in the private home file | `AAP_TOKEN` | Generate if missing |

For the effective entry, apply defaults, its saved settings, selected-file
settings, then environment values. Other entries use their own settings. Read a
stored token only from the private home file, and use it only when its stored URL matches the
effective API origin; `AAP_TOKEN` overrides it. Track presence so explicit `false`
is not treated as missing. An absent default file is acceptable; a missing
explicitly selected file or malformed file is an error. Reject unknown fields
and token fields outside the private home file. Prompt for missing/blank required
values; reject invalid nonempty tokens, invalid booleans/modes, URL credentials,
query strings, fragments, unsupported schemes (anything other than HTTP or HTTPS),
and non-root URL paths.

Keep the token separate from printable settings and never accept it as an
argument. Persist the API URL, token, mode, and TLS setting per connection; do not
save login usernames or passwords. Use directory permissions `0700`, file permissions
`0600`, and atomic writes that preserve unrelated settings. Redact credentials
from errors and output. Display the connection mode and whether TLS verification
is disabled without displaying credentials. Identify HTTP connections and show
that TLS verification is not applicable.

## API behavior and Go types

Use `net/http`, `encoding/json`, and `context`. Inject the HTTP client, configure
request deadlines, and make all operations cancellable. Use `/api/controller/v2/`
for gateway mode and `/api/v2/` for direct mode.

Start job listing with `unified_jobs/`, supplementing only if documented API
behavior shows that an exposed required type is absent. Decode details and related
operations according to resource type. Use server-side search and bounded pages;
do not download entire collections for local filtering. Default to recent jobs
ordered newest first, with explicit pagination to older jobs.

Validate related and pagination links against the configured origin and allowed
controller API routes. Do not forward authentication to arbitrary links or
redirects. Distinguish connection, authentication, permission, malformed-response,
missing-resource, unsupported-operation, and temporary server failures. Wrap
errors with operation context and expose safe messages instead of raw bodies.

| Type | Responsibility |
| --- | --- |
| `Settings`, `ConnectionMode` | Validated non-secret configuration |
| `Client` | Concrete API client and injected dependencies |
| `TemplateRef`, `TemplateType`, `TemplateSummary`, `TemplateDetails` | Typed template resources |
| `JobRef`, `JobType`, `JobStatus` | Job identity, kind, and state |
| `JobSummary`, `JobDetails` | Common fields and explicit optional type-specific details |
| `JobCapabilities` | Output, cancellation, relaunch, and children: availability and reasons |
| `LaunchRequirements`, `SurveyQuestion`, `LaunchResult` | Job-template launch preparation, survey controls, and mutation outcome |
| `WorkflowNode` | Node state and optional child-job reference |
| `ListOptions`, `Page[T]`, `PageCursor` | Search and pagination without HTTP details |
| `OutputCursor`, `OutputChunk`, `OutputUpdate` | Output ranges, resume position, status, and follow errors |
| `OutputStore` | Concrete bounded memory/disk cache with range indexing |
| `APIError`, `ErrorKind` | Classified errors with safe presentation |

Use small TUI consumer interfaces for template reading, job reading, workflow
navigation, output reading/following, cancellation, relaunching, and job-template
launch preparation/submission. Their methods accept contexts and use these types.
Final signatures depend on the offline API contract review; avoid speculative
abstractions and transport-specific UI interfaces.

Capabilities distinguish unsupported operations, unavailable permissions, and
unknown capability. Server responses remain authoritative when permissions or
job state change. Cancellation is a distinct operation from cancelling a local
context. After an ambiguous cancellation response, refresh job state rather than
automatically repeating the mutation.

## Output and lifecycle

Fetch bounded output increments using verified range semantics for each supported
type. Decode output formats in the API layer and sanitize external terminal
control sequences before rendering. Do not assume JSON content is HTML or plain
text without reviewing its contract. Do not assume HTTP byte-range support.

Opening output with `o` starts at the newest available lines. Fetch the latest
bounded window and its absolute output end in one request; do not walk from
line zero to reach it. Keep lines in chronological order within the
viewport. Fetch earlier ranges only when the user scrolls or pages upward.

Maintain a cursor per job/output session. Accept validated chunks into the output
store before advancing the cursor. Track cached ranges separately from the live
cursor so initially unloaded history is allowed and reading older ranges never
moves the live cursor backward. Handle overlapping ranges and mutable partial
lines without duplication. Distinguish unloaded history from unexpected gaps;
surface resets, expired output, and API output size limits explicitly.

Use a bounded viewport/history window in RAM and a bounded, indexed disk cache.
Create private session directories under `~/.cache/aaptui/` using `os.UserHomeDir`,
directory permissions `0700`, and file permissions `0600`. Do not use the system
temporary directory or an XDG cache location outside the user's home directory.
Use random cache names with no tokens or server-returned path components. Cache
files may contain sensitive job output; never copy the authentication token into
cache metadata. Redact the known token if it occurs in displayed or cached text.

Fetch uncached older ranges when scrolling. Eviction must not move the live
cursor backward. If the server no longer retains a requested range, explain the
missing history. Handle disk exhaustion and read/write/cleanup failures explicitly.
An API download-only fallback, if supported, must stream through bounded buffers
into the private cache and observe the disk budget. Select and document memory,
disk, and chunk limits during the output milestone.

Clean up owned cache files when the output session closes or the app exits. Never
delete unrelated files. Interrupted-process leftovers may remain; document their
location and avoid automatic broad cleanup of other sessions.

Polling and retry delays remain in the API package. Retry recoverable failures
with capped backoff, respecting rate-limit guidance where available. Preserve
accepted output and the cursor; show connection state and allow resuming.
Authentication/permission failures stop automatic retries. Use deterministic timing
controls in tests rather than real sleeps.

Scrolling up pauses automatic scrolling, not output collection. An explicit follow
action returns to the newest output with a bounded fetch. Poll for the latest
window while a job is active without automatically downloading intervening
history, and preserve the user's position while reading older output. Leaving the
output screen cancels its context, requests, retry waits, and producer activity.
Bound pending updates and ignore messages from old session identifiers. Use the
completion evidence supported by the API and fetch the final latest window before
stopping; do not equate terminal job status with immediate output completeness or
require downloading all history. Quitting stops all local activity without
cancelling jobs.

Workflow details show child nodes, including pending nodes without a job and
inaccessible/deleted children. Allow navigation into nested workflows and child
output, preserving the back-navigation stack. Jobs without stdout show a clear
unavailable-output state while remaining viewable.

## Navigation

Main contains Job Templates, Jobs, Projects, and Inventories. Use Enter to open,
Esc to return, `q` to quit outside text entry, and Ctrl-C to quit consistently. Search input consumes
ordinary characters; Esc first exits input mode. Provide visible contextual help,
manual refresh where useful, page navigation, and output follow controls. Bind
server cancellation separately and require a prominent confirmation identifying
the job. Expose relaunch as a distinct job action with its own confirmation.
Job-template details expose `L` launch with capability state. The launch form
uses Tab/Shift-Tab for survey fields, the JSON/YAML `extra_vars` editor, and
explicit Launch/Cancel buttons; ordinary typing stays within the focused input.

## Milestones and acceptance tests

### 1. Display host groups under a single Groups key

Status: complete (offline verification).

Inventory host details currently repeat the `Group:` key for every membership.
Replace those repeated fields with one `Groups:` key whose value is a list of
memberships, with one group per line. For example:

```text
Groups:  - 7 · webservers
         - 12 · production
```

Acceptance criteria:

- Render exactly one `Groups:` key for a populated membership page, with each
  visible group listed beneath it. Preserve the existing group ID/name format
  and direct/inherited membership coverage, including parent groups.
- Reuse the existing detail-field renderer and membership reader where practical.
  Keep the list together and readable at narrow and wide terminal widths; wrap
  long names without repeating the key or losing entries.
- Preserve membership counts, `n`/`p` pagination, refresh, detail scrolling, and
  back navigation. List the current page without downloading every membership.
- Preserve distinct loading, empty, permission-denied, unavailable, and failed
  request states. Membership failures must not hide other host details.
- Add regression tests for multiple groups under one key, a single group, empty
  memberships, long names, narrow/wide layouts, and paginated memberships.
- Update the host-details description in `README.md` and the corresponding
  requirement in `SPEC.md` during implementation.

Implemented: host memberships render as one multiline `Groups:` field, retaining
IDs/names, counts, pagination, and independent loading/error states. Regression
tests cover single/multiple groups, long names, and narrow/wide layouts; existing
membership-state and pagination tests remain in place.

### 2. Filter job output by changed or failed tasks

Status: complete (offline verification).

Add output-view options to show only changed tasks or only failed tasks. Keep
unfiltered output as the default and provide a visible way to return to it.

Acceptance criteria:

- Expose three clearly labeled choices in the output view: `All`, `Changed`, and
  `Failed`. Show the active choice and its controls in contextual help. Keep
  existing scrolling, follow, refresh, and back-navigation controls usable.
- `Changed` limits displayed output to tasks with a changed result; `Failed`
  limits it to tasks with a failed result. Retain task identity and the relevant
  result output in chronological order, including multiline diagnostics.
- Use structured task/event outcome metadata rather than matching words or ANSI
  colors in stdout. During implementation, review the API contract for task
  events, changed/failure classification, and mapping events to output. Record
  the supported job types and semantics in `docs/API_CONTRACT.md` before relying
  on them. Include explicit coverage of mixed host results and ignored failures.
- Keep task filters available where the API exposes sufficient metadata. For
  unsupported job types or inaccessible metadata, show a clear unavailable state
  while keeping unfiltered output accessible. Do not present request failures as
  an empty match set.
- Switching filters applies to completed and active jobs, shows the latest
  matching output, and rejects stale results from the previous selection. An
  explicit empty state indicates when no matching tasks are available yet.
  Start each newly opened job output view with `All` selected.
- Preserve opening at the end, on-demand older history, live polling, follow
  mode, and stable scrolling while new output arrives. Apply the active filter
  to newly arriving results and allow paging through older matching output.
- Keep requests, memory, disk caching, and pending updates bounded under the
  existing output limits. Use bounded API queries/pages for matching events;
  do not download all stdout or all events just to filter a large job locally.
  Keep filtered positions/cursors distinct from absolute stdout line positions.
- Preserve request cancellation, retries, stale-session rejection, secret
  redaction, terminal sanitization, private cache permissions, and cleanup.
  Keep API retrieval/classification in `internal/aap` and selection/rendering in
  `internal/tui`, using explicit dependencies and existing patterns.
- Add fake-server and TUI regression tests covering All/Changed/Failed switching,
  matching and nonmatching outcomes, mixed host results, multiline output, no
  matches, active-job updates, history paging, filter changes during in-flight
  requests, unsupported/denied metadata, and bounded work for large logical jobs.
- Update `README.md` and `SPEC.md` during implementation with the filter controls,
  supported job types, outcome semantics, and any remaining limitations.

Implemented: `A` All, `C` Changed, and `F` Failed select output modes. Playbook
and ad hoc task filters use structured host/item outcome flags, task names, and
full bounded event stdout. Eight-event keyset pages support history and live
polling without collecting the full job. Filter sessions keep counters separate
from stdout offsets, cancel on switching/leaving, and reject stale results.
Unsupported types retain All; metadata errors and empty matches are distinct.
Oversized event pages report their limit and offer All. The reviewed contract,
outcome semantics, memory bounds, and offline compatibility limits are recorded
in `docs/API_CONTRACT.md`, `README.md`, and `SPEC.md`.

Verification: changed Go files formatted with `gofmt -w`; `go test ./...`,
`go vet ./...`, `go test -race ./...`, and `git diff --check` pass. Synthetic tests
cover both API prefixes, both supported job types, large logical jobs, mixed
outcomes, ignored failures, multiline diagnostics, polling and completion,
filter switching, stale results, cancellation, errors, and output bounds.

For each implementation milestone: format changed Go files with `gofmt -w`, run
`go test ./...` and `go vet ./...`, review changes for unintended scope, and update
relevant documentation. Use `go test -race ./...` for concurrent output/lifecycle
work. Finish and verify one milestone before moving to the next.

## Verification limits and references

Fixtures must identify whether they are synthetic, source-derived, or captured.
No captured AAP fixtures or deployed-instance acceptance tests are available.
Fake-server success proves behavior against modeled contracts, not compatibility
with an actual AAP installation. Keep this limitation visible in the README.

Prefer AAP 2.6 documentation; use version-pinned upstream AWX source only as
supporting evidence and record that distinction. Public upstream behavior is not
an AAP 2.6 compatibility guarantee. Do not make instance access a completion gate
for the agreed offline implementation.

- [Bubble Tea documentation](https://github.com/charmbracelet/bubbletea)
- [AAP 2.6 job slicing and unified job examples](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/develop-assembly_ug_controller_job_slicing)
- [Upstream AWX output API documentation](https://github.com/ansible/awx/blob/devel/awx/api/templates/api/unified_job_stdout.md)
- [Upstream AWX API implementation](https://github.com/ansible/awx/blob/devel/awx/api/views/__init__.py)

For future API work, record the exact reviewed upstream revision in
`docs/API_CONTRACT.md` before deriving fixtures or relying on its behavior.
