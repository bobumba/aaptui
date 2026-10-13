# AAP TUI Implementation Plan

Status: all milestones, including startup connection setup, are complete and
verified offline. Deployed AAP 2.6 and manual interactive-terminal validation
remain unavailable.

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

Use JSON and the standard library. The default file is
`$XDG_CONFIG_HOME/aaptui/config.json`, falling back to
`~/.config/aaptui/config.json`. Accept `--config PATH` to choose a different file.
Startup setup saves the API URL and token together in
`~/.config/aaptui/config.json`, resolved using `os.UserHomeDir`, regardless of
`XDG_CONFIG_HOME` or `--config`.

| Setting | JSON field | Environment variable | Default |
| --- | --- | --- | --- |
| Server URL | `url` | `AAP_URL` | Prompt if missing |
| Connection mode | `connection_mode` | `AAP_CONNECTION_MODE` | `gateway` |
| TLS verification | `tls_verify` | `AAP_TLS_VERIFY` | `true` |
| Authentication token | `token` in the private home file | `AAP_TOKEN` | Generate if missing |

Apply defaults, then the private home file, then the selected settings file when
it differs from the home file, then environment values. Read a stored token only
from the private home file, and use it only when its stored URL matches the
effective API origin; `AAP_TOKEN` overrides it. Track presence so explicit `false`
is not treated as missing. An absent default file is acceptable; a missing
explicitly selected file or malformed file is an error. Reject unknown fields
and token fields outside the private home file. Prompt for missing/blank required
values; reject invalid nonempty tokens, invalid booleans/modes, URL credentials,
query strings, fragments, unsupported schemes (anything other than HTTP or HTTPS),
and non-root URL paths.

Keep the token separate from printable settings and never accept it as an
argument. Persist only the API URL and token as credentials; do not save login
usernames or passwords. Use directory permissions `0700`, file permissions
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

Maintain a cursor per job/output session. Accept validated chunks into the output
store before advancing the cursor. Handle overlapping ranges and mutable partial
lines without duplication. Surface gaps, resets, expired output, and API output
size limits explicitly instead of silently dropping data.

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
action returns to the newest output. Leaving the output screen cancels its context,
requests, retry waits, and producer activity. Bound pending updates and ignore
messages from old session identifiers. Drain final output using the completion
evidence supported by the API; do not equate terminal job status with immediate
output completeness. Quitting stops all local activity without cancelling jobs.

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

### 1. Prompt for missing connection settings and persist an API token

On startup, resolve the existing connection settings and token sources. If the
server URL or API token is missing, prompt interactively for the missing host URL
and, when a token is needed, the user's username and password. Use those login
credentials to generate an API token, then save the API URL and token together
in `~/.config/aaptui/config.json` for subsequent launches, as specified in
`SPEC.md`. Never save the username or password.

Acceptance criteria:

- Launch directly when a complete URL and token are available. Preserve supplied
  values and prompt only for missing information; a supplied token requires no
  username/password prompt. Connection mode and TLS verification retain their
  existing defaults and validation.
- Distinguish missing settings from invalid settings. Preserve errors for
  malformed files, missing explicitly selected files, invalid URLs, invalid
  modes/booleans, and invalid nonempty tokens. Validate prompted URLs with the
  same rules as configured URLs before sending credentials.
- Mask password entry, support cancellation and EOF, and restore terminal state
  on every exit path. If required values are missing without an interactive
  terminal, return a clear error without waiting for input.
- Review and record the AAP 2.6 token-creation contract for gateway and direct
  connections in `docs/API_CONTRACT.md` before implementing requests or deriving
  fixtures. Explain unsupported authentication/deployment configurations without
  assuming both connection modes share the same token endpoint.
- Keep token generation in `internal/aap`, persistence in `internal/config`, and
  startup orchestration and terminal handling in `cmd/aaptui`. Use explicit
  dependencies, cancellable requests, existing TLS settings, and origin/redirect
  protections. Do not automatically retry a token-creation request with an
  uncertain outcome.
- Save the API URL and generated token only after successful authentication.
  Use a private application directory with permissions `0700` and credential
  files with permissions `0600`, perform atomic writes, preserve unrelated
  settings, and report storage failures without exposing secrets. Keep the
  credential store under the home directory even when `XDG_CONFIG_HOME` or
  `--config` points elsewhere.
- Load saved credentials on later launches and preserve environment overrides.
  Scope saved tokens to their API origin so changing the URL cannot send a saved
  token to a different server. Never accept tokens as command-line arguments or
  expose usernames, passwords, or tokens in application output, errors, or cache
  metadata.
- Implement the private credential-storage path, schema, and precedence defined
  in `SPEC.md` and this plan's Configuration section. Update `README.md` and the
  API contract during implementation to describe first-run setup, credential
  reuse, removal/replacement, and offline verification limits.
- Add regression tests for complete settings, each missing URL/token combination,
  masked entry and cancellation, noninteractive startup, safe token-generation
  failures, both connection modes, credential round trips, permissions, atomic
  writes and storage failures, environment precedence, origin scoping, and
  credential redaction. Use fake servers and temporary homes; do not write tests
  against the developer's real home directory.

Implemented and verified offline: startup prompts for missing settings, masks
passwords, creates gateway tokens with one request, and atomically stores
origin-bound credentials in the private home configuration. Complete supplied
connections launch directly; URL-only setup verifies the existing token before
saving. Environment and selected-file precedence, noninteractive failures, and
safe storage/authentication errors are covered by regression tests. Real-PTY
tests verify masking and terminal restoration after completion, Escape, Ctrl-C,
Ctrl-D, and signal cancellation; input EOF quits without waiting. Signal handling
uses graceful prompt shutdown to avoid asynchronous terminal-reader cleanup.
Direct mode retains supplied-token support and explains that prompted creation
requires gateway mode. `go test ./...`, `go vet ./...`, and `go test -race ./...`
pass. README, specification, and API contract are synchronized.

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
