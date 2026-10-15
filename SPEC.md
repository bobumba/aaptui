# AAP TUI Specification

## Purpose

Build a terminal user interface for interacting with
Ansible Automation Platform (AAP).

The application should allow an administrator to perform
common AAP operations without using the web interface.

## Supported Platform and Runtime

- Support Ansible Automation Platform 2.6 only.
- Connect through either the platform gateway or directly to
  automation controller.
- Run interactively in a user's terminal from a Bash shell.
- Use an existing API token when available, otherwise generate one
  from interactively supplied login credentials, and respect the
  permissions associated with the token.

## Configuration

The application connects to an existing AAP instance.

Configuration:

- AAP server URL
- gateway or direct-controller connection mode
- TLS verification setting

These connection settings may be supplied through environment
variables or a configuration file.

On every launch, display a selectable list of saved AAP URLs from the user's
configuration before opening the main TUI, replacing "Load current configuration".
Selecting a URL reuses that connection's settings and credentials, prompting for
username/password when needed. Show the list even when only one URL is saved.
Mark invalid saved connections as unavailable and keep "Create new configuration"
accessible, including when no URLs are saved. Support multiple saved connections
and continue reading existing single-connection configuration files.

Creating a new configuration prompts for the host URL, connection mode,
TLS verification, username, and password, even when current settings are complete.
Start with gateway mode and TLS verification enabled; do not reuse current URLs
or tokens. Submitting the credentials triggers one token-creation request using
the selected mode's endpoint, then saves the returned token with the connection
settings after success. Mask
password entry and keep login credentials in memory only. Accept gateway or
direct-controller mode; actual endpoint availability and token-creation permissions
depend on the deployment. Validate all settings before sending credentials.
New setup uses the entered values for this launch, ignoring selected-file and
environment overrides. List the effective selected-file/environment URL alongside
saved URLs and apply its overrides only to its matching origin. Selecting another
saved URL uses that connection's settings and token.

Save each API URL and its authenticated token together in
`~/.config/aaptui/config.json`, using the user's home directory and private
directory/file permissions (`0700`/`0600`). Store entries in a JSON `connections`
array with `url`, `token`, `connection_mode`, and `tls_verify`. Read existing
single-connection files and migrate them only on a successful save. Saving the
same API origin updates its entry; distinct origins remain separate. Write
atomically and preserve other saved connections and unrelated settings. This
credential-storage path stays
under the user's home directory even when `XDG_CONFIG_HOME` or `--config`
selects a different settings file. Never persist the login username or
password or accept tokens as command-line arguments.

Use the first selected-file URL, or the first saved URL, as the effective URL,
with `AAP_URL` taking precedence. Start from defaults and that origin's saved
settings, then apply selected-file and environment overrides to that entry only.
List other saved and selected-file URLs separately, preserving their settings.
Reject duplicate origins within a file and mixed legacy URL/token and collection
formats. Optional top-level mode/TLS settings provide defaults for array entries.
Read saved tokens only from the private home configuration, each bound to its
stored API URL;
do not reuse a saved token for a different API origin. `AAP_TOKEN` remains
supported and overrides the saved token. Missing values trigger prompts;
invalid nonempty values and malformed files remain errors. Save new connections
only after successful authentication; cancellation, authentication failures, and
failed replacement writes leave existing credentials intact. Do not overwrite a
malformed or symlinked home credential file; report the storage error so the user
can correct it. Noninteractive startup must fail clearly without waiting for
input, including when credentials are complete. Cancellation and failures must
restore terminal state and must not expose credentials.

Prompted token creation uses `/api/gateway/v1/tokens/` in gateway mode or
`/api/v2/tokens/` in direct-controller mode, with write scope, subject to the
user's existing permissions. Direct mode sends `application: null` to create a
personal token. Gateway is the preferred AAP 2.6 route; direct-controller OAuth
is a deployment-dependent compatibility path. Keep requests cancellable,
reject redirects, and never automatically retry uncertain token creation.
When using `AAP_TOKEN`, supply a URL so it appears in the selection list.
Selecting a complete saved connection performs no startup authentication request
or credential write.

Credentials must never be stored in source code or displayed in
application output. TLS verification must be enabled by default.

## Initial Features

### Connection header

Display the effective connected AAP URL and the authenticated user's username
from the `me/` endpoint on every screen. Lookup is asynchronous, with visible
loading and error states, and must not display credentials.
Also display the total number of jobs with status `running` visible to the
authenticated token across supported job types. Refresh the count periodically
without blocking navigation; failures must mark it stale or unavailable.
Quitting stops header requests and polling.

### Job Templates

Users can:

- List job templates
- Search/filter job templates
- View job template details
- View a paginated table of the selected template's jobs, newest first
- Open a job from that table and return to the template's previous context
- Launch a job with `L`, an enabled survey, and manually defined JSON/YAML `extra_vars`

The focused launch form supports text, textarea, password, integer, float,
multiplechoice, and multiselect survey questions, including defaults, required
answers, and bounds. Tab and Shift-Tab move through survey fields, the multiline
`extra_vars` editor, and Launch/Cancel controls. Enter edits multiline input or
activates a focused control; it never submits from a survey or variables field.
Mask password input and preserve controller password defaults without displaying
their values. Ordinary text, including `q`, stays in the focused input.

Use a centered, rounded-border launch dialog consistent with cancel/relaunch
prompts. Give every survey control and the manual variables editor a separate
box, with a double border and `>` marker for focus in both light and dark themes.
Keep labels, required/optional state, descriptions, and validation feedback
associated with their inputs. Show launch progress and safe failures within the
dialog and keep Launch/Cancel controls visible where space permits. Scroll to
keep the focused field and cursor visible when navigating or resizing; simplify
decoration on small terminals before sacrificing editing or dismissal controls.

The manual extra-vars editor starts empty, with its cursor at the beginning;
blank or whitespace-only input adds no manual overrides and retains controller
template defaults and required-variable checks. Manual variables accept one
bounded JSON/YAML mapping. Reject duplicate keys and survey/manual collisions.
Unrestricted manual variables require the template's
Prompt on launch setting. Omit optional blank answers according to the reviewed
contract. Recheck permissions and current requirements before launching; explain
unsupported required inventory, credential, or credential-password inputs.
Submission explicitly creates a new execution, sends at most one POST, and opens
the new job (or sliced workflow job), preserving the template's previous context.
Report ignored inputs, retain inputs after definite failures, and disable retries
after uncertain outcomes until the user closes the form and checks Jobs. Form
inputs remain in memory only. Dismissal before submission sends no mutation.

Existing jobs can be relaunched from job details where supported by the API and
permitted by the user's token. Workflow-template launching remains outside scope.

### Projects

Users can:

- List projects through the projects API endpoint
- Search/filter projects and navigate pages
- View project details, including status and SCM settings

### Inventories

Users can list and search inventories, navigate pages, and select inventories.
Selecting an inventory opens Groups and Hosts. Hosts lists all visible inventory
hosts across pages. Groups lists the inventory's groups; selecting a group opens
Child groups and Hosts, allowing nested traversal. Group Hosts includes hosts in
descendant groups. Esc restores the previous selection, page, and traversal context.

### Jobs

Users can:

- List recent jobs
- View job status and start/end date-times in the Jobs list
- View job details
- View job output
- Continuously follow live output while a job is active, without
  requiring manual refresh
- Cancel an active job when supported by its type and permitted
  by the user's token
- Relaunch an existing playbook job, workflow job, or ad hoc command when its
  relaunch endpoint, permissions, and required inputs permit it

Cancellation requires a prominent, focused confirmation prompt identifying the
job by name, ID, and type. Dismissal performs no mutation; confirmation performs
at most one request and displays progress and the result. Background actions and
refresh are blocked while the request is pending; Esc and quit remain available.

Job details show relaunch (`R`) separately from refresh (`r`), including capability
state and reasons. Relaunch requires a focused confirmation identifying the
source job and explaining that it creates a new execution with the original
settings and limit. Recheck permissions and required inputs before one POST;
jobs requiring credential passwords direct users to the controller UI. Open the
new job's details on success and preserve the source in back navigation. Keep
the new identity visible if its details cannot be read. Never retry ambiguous
relaunch responses automatically; explain the uncertain outcome and allow
refreshing Jobs before a manual retry. Project/inventory updates, system jobs,
approvals, and unknown types remain unavailable for relaunch.

The Jobs view must include all job types exposed by automation
controller, rather than only playbook jobs. This includes workflow
jobs, project updates, inventory updates, ad hoc commands, system
jobs, and workflow approvals where exposed by the API.

Job details, cancellation, and output handling must account for
differences between job types. Job types without standard output
must remain viewable and clearly indicate that output is unavailable.
Workflow jobs must provide navigation to their child jobs and the
output available on those jobs.

Opening output with `o` must show the latest bounded window in chronological
order, without downloading or replaying the entire job log. Fetch earlier ranges
only as the user scrolls or pages up, reusing cached history within the existing
resource budgets. Live polling and `f` follow must retrieve the latest window
directly, without automatically backfilling skipped history. Reading older output
must preserve the visible position while polling continues.

The output view provides uppercase `A` All (default), `C` Changed, and `F`
Failed, with the active filter visible. Lowercase `f` retains follow mode.
Playbook jobs and ad hoc commands filter structured host/item result events using their stored changed/failed flags and retain task
identity and multiline diagnostics. Mixed host outcomes show matching results
only; changed failures can match both filters. Failed includes API-flagged
unreachable/error results and excludes ignored failures whose flag is false.
Unsupported job types and inaccessible metadata show a clear unavailable/error
state while All remains accessible. Empty matches have a distinct state.
Each newly opened output view defaults to All.

Task filtering uses bounded server queries and separate event-counter cursors,
opens at the latest matches, and retrieves older matches on demand. Preserve
scrolling position during live polling and reject stale results after switching
filters. Each page contains up to eight events, bounded to 256 KiB and 65,536
lines; render at most 256 lines per viewport. Keep pages in memory only, with no
accumulating history cache. Oversized pages fail explicitly and offer All output.

Live output must support scrolling through received output and
following newly arriving output. Recoverable connection failures
must be displayed clearly, and following must be resumable without
duplicating already displayed output. Leaving the output screen or
quitting must stop its background activity. Exiting the application
must not cancel jobs on the server.

### Object details

Object views show pertinent common and type-specific fields selected through an
endpoint-by-endpoint API review. Include available timestamps, ownership and
related resource names/IDs, execution settings, SCM revisions and update settings,
inventory counts and failure flags, host state, and status explanations. Preserve
false/zero values and handle absent fields without inventing data. Do not display
credential contents, variables outside jobs and job templates, environments, module
arguments, or artifacts. Sanitize external text and redact the configured token.

Job views place common and type-specific fields under Job details. A borderless
`extra_vars` row displays the recorded mapping as YAML for playbook
jobs, workflow jobs, ad hoc commands, and system jobs, including survey answers.
Use execution values rather than current template defaults or survey definitions;
do not infer variable origins. Show sorted names and readable values, preserving
nested structures, number precision, false/zero/null, blank strings, and multiline
answers. Use indented YAML mappings and lists, quote keys and scalars as needed
to preserve their types, and use block scalars for multiline answers where
possible. The YAML spans the full table width, with indentation and spaces
preserved when wrapping. Preserve server password masks and sanitize/redact
recursively before display. Keep values in memory only, with 64 KiB decoded-input
and display budgets and at most 64 nesting levels. Distinguish empty, unavailable, malformed,
oversized, and unsupported data without hiding other job fields.

Job-template details display configured `extra_vars` in an Extra variables box,
using the same decoding, display limits, and sanitization as recorded job values.
Do not fetch current survey definitions or launch requirements for this display.
Retain formatted JSON for nested template values and line breaks for multiline
answers. Group template variables or the data-state message inside one clearly
labeled, theme-aware bordered box. Wrap its contents to the available width
and use existing detail scrolling and resizing; omit decoration on extremely
narrow terminals. Keep other resource variables excluded.
In job-template details, `r` refreshes the focused details or job table without
discarding the other pane's state.

A separate Capabilities section identifies output (`o`), workflow children
(`w`), cancellation (`c`), and relaunch (`R`), their availability, and any reasons.
Preserve existing navigation and require focused confirmation for cancellation
and relaunch, or explicit submission of the focused template-launch form.

All resource detail views use a consistent responsive table with aligned label
and value columns. Field names are bold and use a theme-aware color distinct from
values. Wide terminals show multiple field/value pairs per row; narrow terminals
use one pair per row and very small widths stack labels above values. Descriptions
and multiline values span the table width. Wrap long labels and values using
Unicode display widths, and keep every wrapped row accessible through scrolling.
Resizing and scrolling preserve existing menu/table focus and navigation context.

Inventory/group screens retain their navigation menus; Tab switches menu/details
focus. Hosts have scrollable detail views with a Groups section listing visible
direct and inherited memberships, including parent groups, under one `Groups:`
key with one group per list entry. Use `n`/`p` to page
groups on demand and `r` to refresh details and memberships. Membership requests
run independently of host detail requests; loading, empty, unavailable, permission,
and failure states remain distinct, and stale responses are ignored. Workflow
nodes, including pending and skipped nodes, have inspectable details through `d`,
with existing Enter child-job
navigation retained. Detail requests are asynchronous and cancellable, and Esc
restores prior selection, search, page, and detail context.

### Navigation

The application should be keyboard driven.

Initial screens:

Main
├── Job Templates
├── Jobs
├── Projects
└── Inventories
    └── Selected inventory
        ├── Groups → Selected group → Child groups / Hosts
        └── Hosts

The user must be able to return to the previous screen
and quit the application using consistent key bindings.

Output paging accepts Ctrl-U and Ctrl-D as aliases for Page Up and Page Down,
respectively, with the same paging distance and follow-mode behavior.

## AAP API

Interact with AAP through its REST API.

Use `/api/controller/v2/` for gateway connections and `/api/v2/`
for direct-controller connections. Any additional transport needed
for live output must remain within the API layer; the live-output
transport will be finalized during architecture review.

Keep API communication separate from TUI code.

The API layer should expose Go functions/types rather than
requiring the UI to understand HTTP details.

## Error Handling

API errors should be displayed clearly without terminating
the application when possible.

Connection failures, authentication failures, and malformed
API responses should produce useful error messages.
