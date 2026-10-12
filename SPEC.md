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
- Use the user's existing API token and respect the permissions
  associated with that token.

## Configuration

The application connects to an existing AAP instance.

Configuration:

- AAP server URL
- gateway or direct-controller connection mode
- TLS verification setting

These connection settings may be supplied through environment
variables or a configuration file.

The authentication token must be supplied through the `AAP_TOKEN`
environment variable. Do not accept or persist the token in the
configuration file or command-line arguments.

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

Launching or relaunching jobs is deferred and is not part of the
initial implementation.

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

The Jobs view must include all job types exposed by automation
controller, rather than only playbook jobs. This includes workflow
jobs, project updates, inventory updates, ad hoc commands, system
jobs, and workflow approvals where exposed by the API.

Job details, cancellation, and output handling must account for
differences between job types. Job types without standard output
must remain viewable and clearly indicate that output is unavailable.
Workflow jobs must provide navigation to their child jobs and the
output available on those jobs.

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
credential contents or arbitrary secret-bearing variables, environments, module
arguments, or artifacts. Sanitize external text and redact the configured token.

Job views place all displayed API-returned job fields under one Job details
section. A separate Capabilities section identifies output (`o`), workflow children
(`w`), and cancellation (`c`), their availability, and any reasons. Preserve the
existing navigation and cancellation confirmation behavior.

All resource detail views use a consistent responsive table with aligned label
and value columns. Field names are bold and use a theme-aware color distinct from
values. Wide terminals show multiple field/value pairs per row; narrow terminals
use one pair per row and very small widths stack labels above values. Descriptions
and multiline values span the table width. Wrap long labels and values using
Unicode display widths, and keep every wrapped row accessible through scrolling.
Resizing and scrolling preserve existing menu/table focus and navigation context.

Inventory/group screens retain their navigation menus; Tab switches menu/details
focus. Hosts have scrollable detail views with a Groups section listing visible
direct and inherited memberships, including parent groups. Use `n`/`p` to page
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
