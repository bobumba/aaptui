# AAP TUI Implementation Plan

Status: all milestones, including launch prompt presentation, are implemented
and verified offline. Deployed AAP 2.6 validation remains unavailable.

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

`internal/config` loads connection settings and reads `AAP_TOKEN` separately.
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

| Setting | JSON field | Environment variable | Default |
| --- | --- | --- | --- |
| Server URL | `url` | `AAP_URL` | Required |
| Connection mode | `connection_mode` | `AAP_CONNECTION_MODE` | `gateway` |
| TLS verification | `tls_verify` | `AAP_TLS_VERIFY` | `true` |
| Authentication token | Forbidden | `AAP_TOKEN` | Required |

Apply defaults, then file values, then environment values. Track presence so
explicit `false` is not treated as missing. An absent default file is acceptable;
a missing explicitly selected file or malformed file is an error. Reject unknown
configuration fields, including token fields. Reject empty required values,
invalid booleans/modes, URL credentials, query strings, fragments, and unsupported
schemes (anything other than HTTP or HTTPS) or non-root URL paths.

Keep the token separate from printable settings, never persist it or accept it
as an argument, and redact it from error text. Display the connection mode and
whether TLS verification is disabled without displaying credentials. Identify
HTTP connections and show that TLS verification is not applicable.

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

### 1. Improve job launch prompts

Replace the rudimentary job launch form presentation with a focused, bordered
prompt styled consistently with the existing job cancel/relaunch confirmation
prompts. Give each survey field and the manual `extra_vars` editor its own
distinctive box so inputs and focus are easy to identify.

Acceptance criteria:

- Present the launch form in a centered, rounded-border panel where terminal
  dimensions permit, following the cancel/relaunch prompts' spacing and styling
  conventions. Keep the template name and ID, the new-execution notice, and
  explicit Launch/Cancel controls clear.
- Render every survey control in a separate bordered box, including text,
  password, numeric, choice, multiselect, and multiline inputs. Give the manual
  JSON/YAML `extra_vars` editor its own bordered box. Keep labels, descriptions,
  required/optional indicators, choices, and validation feedback visibly
  associated with their fields.
- Distinguish the focused field or action with the existing theme's styles and
  a visible non-color cue. Keep boxes and focus legible in light and dark themes.
- Preserve Tab/Shift-Tab navigation, ordinary typing within the focused control,
  multiline editing, password masking, variable validation, and explicit
  submission. Esc dismisses without launching; Ctrl-C quits.
- Account for borders and padding when sizing and scrolling the form. Keep the
  focused input and cursor visible as focus moves or the terminal resizes.
  On narrow or short terminals, simplify decoration while keeping inputs,
  actions, and contextual help usable without overflowing the frame.
- Show loading, submission, validation, and safe failure states within the prompt.
  Preserve existing launch permissions, variable handling, duplicate-submission
  prevention, ambiguous-outcome handling, and successful/back navigation.
- Add focused TUI regression tests for the bordered panel and field boxes,
  focus visibility, scrolling/resizing, narrow and short terminals, templates
  with and without surveys, and retained editing/dismissal/submission behavior.
  Use existing launch tests and fake readers; no API contract changes are planned.
- Update relevant user documentation and contextual help to reflect the prompt
  presentation and any layout-specific navigation guidance.

Implemented and verified offline: launch uses a centered rounded-border dialog,
separate survey/variables boxes, double borders and text markers for focus,
field-specific validation feedback, and visible action controls. Scrolling and
resizing keep the focused field and cursor visible; small terminals reduce
decoration and prioritize editing and dismissal. Existing launch behavior is
preserved. TUI regression tests cover all survey control types, templates without
surveys, both themes, focus and resizing, progress/errors, and ambiguous outcomes.
`go test ./...` and `go vet ./...` pass. README, specification, and contextual help
are synchronized. Deployed AAP 2.6 and interactive terminal validation remain
unavailable.

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
