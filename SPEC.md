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

### Job Templates

Users can:

- List job templates
- Search/filter job templates
- View job template details

Launching or relaunching jobs is deferred and is not part of the
initial implementation.

### Jobs

Users can:

- List recent jobs
- View job status
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

### Navigation

The application should be keyboard driven.

Initial screens:

Main
├── Job Templates
└── Jobs

The user must be able to return to the previous screen
and quit the application using consistent key bindings.

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
