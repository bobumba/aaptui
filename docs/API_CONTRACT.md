# Offline API contract review

Reviewed 2026-10-02. No deployed AAP 2.6 instance or captured fixtures are available.
All test fixtures are synthetic models of the reviewed contracts. Passing them is
not an AAP compatibility guarantee.

## Evidence

AAP 2.6 [API changes](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/upgrade-assembly_upgrade_api_changes)
and [API browsing](https://docs.redhat.com/en/documentation/red_hat_ansible_automation_platform/2.6/develop-assembly_controller_api_browsing_api)
document the gateway controller prefix. Direct `/api/v2/` is retained per scope;
direct token acceptance depends on the deployment.

Supporting upstream evidence: AWX tag **24.6.1**, commit `94e5795dfc37b95c576d61f3e3b4e936c021548c`, reviewed files
`awx/api/views/__init__.py`, `awx/api/serializers.py`, `awx/api/generics.py`,
`awx/main/models/unified_jobs.py`, and `awx/api/templates/api/unified_job_stdout.md`.
[Version-pinned source](https://github.com/ansible/awx/tree/24.6.1).
Upstream behavior below is an assumption for AAP 2.6, not product evidence.

## Resources

Header and template-job contracts reviewed 2026-10-12 against the same AWX
24.6.1 commit `94e5795dfc37b95c576d61f3e3b4e936c021548c`.
`UserMeList` returns a paginated collection containing the requesting user;
read `me/` and require one result with a positive `id` and nonempty `username`.
`JobTemplateJobsList` reads `job_templates/{id}/jobs/`, with the ordinary page
envelope and ordering options. `UnifiedJobList`, field filtering, and pagination
provide `unified_jobs/?status=running&page_size=1` and a total `count` across
jobs visible to the requesting user. These are upstream assumptions for AAP 2.6.
Supporting files: `awx/api/views/__init__.py`, `awx/api/urls/urls.py`,
`awx/api/urls/job_template.py`, `awx/api/generics.py`, `awx/settings/defaults.py`,
and `awx/api/pagination.py`
at the pinned revision. New fixtures are synthetic. The count polls every five
seconds, with cancellable requests/waits and capped backoff for recoverable
failures. Failed refreshes retain a visibly stale count; authentication,
permission, and malformed-response failures stop automatic polling.

Prepend `/api/controller/v2/` (gateway) or `/api/v2/` (direct) to each route.
The Job Templates menu lists only `job_templates/`; the Jobs menu lists all job
types through `unified_jobs/`; the Projects menu lists `projects/` and reads
details from `projects/{id}/`. These collections use
`count`, `next`, `previous`, `results`; query `search`, `page_size`, `order_by`.
No supplement is justified by reviewed evidence: upstream polymorphic serializers
include approvals even though their advertised type lists omit them.

| Job type | Detail collection | Template type / collection | Stdout | Cancel | Children |
| --- | --- | --- | --- | --- | --- |
| job | jobs | job_template / job_templates | ranged JSON | GET/POST cancel | none |
| workflow_job | workflow_jobs | workflow_job_template / workflow_job_templates | unavailable | GET/POST cancel | workflow_nodes |
| project_update | project_updates | project / projects | ranged JSON | GET/POST cancel | none |
| inventory_update | inventory_updates | inventory_source / inventory_sources | ranged JSON | GET/POST cancel | none |
| ad_hoc_command | ad_hoc_commands | none | ranged JSON | GET/POST cancel | none |
| system_job | system_jobs | system_job_template / system_job_templates | inline result_stdout, or ranged JSON if exposed | GET/POST cancel | none |
| workflow_approval | workflow_approvals | workflow_approval_template / workflow_approval_templates if exposed | unavailable | unsupported | none |
| unknown | unified_jobs | exposed unknown template / unified_job_templates | unknown | unknown | unknown |

Use type-specific detail routes and validated related links. Missing optional
fields do not make resources disappear. Unknown types retain common details.
`related.cancel` indicates an endpoint, not permission. GET that endpoint checks
cancel permission and returns `can_cancel`; POST can still reject a state change.
Approval/deny endpoints are distinct and outside scope.

Workflow nodes include `id`, `identifier`, nullable `job`, `do_not_run`,
`related.job`, and optional `summary_fields.job` type/name/status. Null jobs are
pending or skipped. Children with missing summaries can be resolved by unified
ID; permission/deletion errors remain visible. Nested workflows use the same
navigation and retain their parent stack.

## Inventories

The inventory list reads `inventories/` using the existing bounded pagination
contract. Resources expose `id`, `type: inventory`, `name`, `description`, and
`kind` (empty for a standard inventory). Supporting evidence is AWX **24.6.1**,
commit `94e5795dfc37b95c576d61f3e3b4e936c021548c`,
[`awx/api/urls/inventory.py`](https://github.com/ansible/awx/blob/24.6.1/awx/api/urls/inventory.py)
and `awx/api/serializers.py`. Fixtures are synthetic; AAP 2.6 remains unverified.

Inventory Groups reads `inventories/{id}/groups/` (all groups, rather than only
roots). Inventory Hosts reads `inventories/{id}/hosts/`. A group's child list
reads `groups/{id}/children/`; its Hosts list reads `groups/{id}/all_hosts/`,
including descendants. These collections use the same bounded search and page
contract. Groups and hosts expose `inventory`; hosts also expose `enabled`.
Smart inventories may include hosts owned by a different inventory, so host
ownership is not compared with the selected inventory. Server permission and
missing-resource responses remain authoritative, including unavailable groups.

Supporting evidence at the same upstream revision:
[`awx/api/urls/group.py`](https://github.com/ansible/awx/blob/24.6.1/awx/api/urls/group.py),
`InventoryGroupsList`, `InventoryHostsList`, `GroupChildrenList`, and
`GroupAllHostsList` in `awx/api/views/__init__.py`, plus `GroupSerializer` and
`HostSerializer` in `awx/api/serializers.py`. No inventory mutations are performed.

## Object field review

Reviewed 2026-10-12 against AWX **24.6.1**, commit
`94e5795dfc37b95c576d61f3e3b4e936c021548c`:
[serializers and summary-field definitions](https://github.com/ansible/awx/blob/94e5795dfc37b95c576d61f3e3b4e936c021548c/awx/api/serializers.py),
[inventory routes](https://github.com/ansible/awx/blob/24.6.1/awx/api/urls/inventory.py),
[group routes](https://github.com/ansible/awx/blob/24.6.1/awx/api/urls/group.py), and
[host routes](https://github.com/ansible/awx/blob/94e5795dfc37b95c576d61f3e3b4e936c021548c/awx/api/urls/host.py).
The field inventory below records selections from inherited serializer fields,
type-specific fields, and `summary_fields`. It is upstream supporting evidence;
returned fields on a deployed AAP 2.6 installation remain unverified. New test
responses are synthetic. Both configured API prefixes use the same field model.

Common object fields are `id`, `type`, `name`, `description`, `created`, and
`modified`, where returned. Ownership uses `organization` and the returned
`summary_fields.organization`, `created_by`, and `modified_by` IDs/names/usernames.
Related resources retain an ID if their summary is unavailable; mismatched summary
IDs never supply a name for a different resource. No additional related-resource
requests are needed. Credential references use only IDs/names from `credential`
and `summary_fields.credential`/`credentials`; credential contents are excluded.

The execution fields selected where applicable are `job_type`, `inventory`,
`project`, `execution_environment`, `limit`, `scm_branch`, `forks`, `verbosity`,
`timeout`, `job_tags`, `skip_tags`, `start_at_task`, `job_slice_count`,
`become_enabled`, `diff_mode`, `force_handlers`, `use_fact_cache`,
`allow_simultaneous`, and `survey_enabled`. Related names and the resolved
execution environment use whitelisted summary fields. Optional numbers and
booleans use pointers, so omitted/null differs from zero/false. Missing times
show a placeholder; invalid times show `Unavailable`. Displayed times use UTC.

| Endpoint(s), relative to configured prefix | Pertinent returned fields selected | Returned fields omitted and reason |
| --- | --- | --- |
| `unified_jobs/`, `job_templates/{id}/jobs/` | Existing ID/name/type/status; main Jobs list also displays `started` and `finished`. Details come from the type-specific endpoint below. | Other detail fields stay out of the list to keep search and pagination readable. |
| `jobs/{id}/` | Common metadata and execution fields; `playbook`, `scm_revision`, `job_slice_number`, and common job state below. | `extra_vars`, `artifacts`, `passwords_needed_to_start`, webhook identifiers/credentials and launch internals can contain sensitive data or describe unsupported launch/edit operations. |
| `workflow_jobs/{id}/` | Common metadata and job state; available inventory, limit, branch, tags, and simultaneous-execution settings. | `extra_vars` and webhook data are excluded; workflow children retain separate navigation. `is_sliced_job` duplicates the slice/ancestor context already available. |
| `project_updates/{id}/` | Common job state, project reference, job type/tags, SCM type, repository location, branch, revision, refspec, local path, clean/submodule/delete flags, and timeout. | Raw credential contents and custom runtime paths/settings do not aid supported browsing operations. |
| `inventory_updates/{id}/` | Common job state, inventory/source references, source/path/branch/revision, host filter, overwrite flags, verbosity/limit/timeout, `license_error`, and `org_host_limit_error`. | `source_vars`, enabled-variable values, custom runtime configuration, and intermediate update/placement references are excluded as sensitive configuration or low-value implementation detail. |
| `ad_hoc_commands/{id}/` | Common job state, `module_name`, inventory/credential references, job type, limit, forks, verbosity, become and diff flags. | `module_args` and `extra_vars` may contain secrets; module arguments are no longer rendered. |
| `system_jobs/{id}/` | Common job state and `job_type`; existing bounded stdout handling remains separate. | `extra_vars` are arbitrary secret-bearing configuration; `result_stdout` belongs in the output view rather than details. |
| `workflow_approvals/{id}/` | Common job state, available timeout, `approval_expiration`, and `timed_out`. | `can_approve_or_deny` describes approval mutations outside scope. |
| `unified_jobs/{id}/` (unknown types) | Common metadata, launch/status/failure/timing fields, returned execution settings, template/ancestor/workflow references, and explicit capability state. | Unknown type-specific payloads are not dumped as arbitrary JSON. |
| `job_templates/`, `job_templates/{id}/` | Existing list summary; details add common metadata, run history, playbook, execution settings, survey/become/diff/simultaneous flags, and credential references. | `host_config_key`, `extra_vars`, webhook settings, legacy runtime configuration and launch-prompt flags are excluded as secrets or unsupported launch/edit configuration. |
| `workflow_job_templates/{id}/` | Common metadata/run history, inventory, limit, branch/tags, survey and simultaneous-execution flags. | `extra_vars`, webhook data and launch-prompt flags have the same exclusions as job templates. |
| `projects/`, `projects/{id}/` | Existing list summary; details add common metadata/run history, SCM type/repository/branch/revision/refspec/local path, clean/submodule/delete/update flags, cache timeout, branch override, timeout and default environment/credential references. | `last_updated` and `last_update_failed` are compatibility aliases for run history; legacy virtualenv and signature-validation configuration are outside supported operations. Repository URLs omit userinfo, query and fragment. |
| `inventory_sources/{id}/` | Common metadata/run history, inventory/source-project references, source/path/branch/host filter, overwrite and update flags, cache timeout, verbosity/limit/timeout and credential references. | `source_vars`, enabled-variable values and custom runtime configuration are excluded; update history aliases duplicate run history. |
| `system_job_templates/{id}/` | Common metadata/run history and `job_type`. | Raw related links and role/capability maps are not displayed as JSON. |
| `workflow_approval_templates/{id}/` | Common metadata/run history and `timeout`. | Related launch/edit operations remain out of scope. |
| `unified_job_templates/{id}/` (unknown types) | Common metadata/run history and returned execution settings. | Unknown payloads are not dumped as arbitrary JSON. |
| `inventories/`, `inventories/{id}/` | Existing list summary; detail read adds common metadata, `kind`, `host_filter`, `total_hosts`, `total_groups`, `total_inventory_sources`, `hosts_with_active_failures`, `inventory_sources_with_failures`, `has_active_failures`, `has_inventory_sources`, `pending_deletion`, `prevent_instance_group_fallback`. | `variables` may contain secrets. Other relationship/role links are not needed for the retained Groups/Hosts menu. |
| `inventories/{id}/groups/`, `groups/{id}/children/`, `groups/{id}/` | Existing group list summary; detail read adds description, created/modified/owner summaries and inventory reference. | `variables` may contain secrets; raw relationship/role maps are replaced by existing child-group/host navigation. |
| `inventories/{id}/hosts/`, `groups/{id}/all_hosts/`, `hosts/{id}/` | Existing host list summary/enabled state; detail read adds metadata, inventory reference, `instance_id`, `has_active_failures`, `has_inventory_sources`, `last_job`, `last_job_host_summary`, and `ansible_facts_modified`. | `variables`, raw facts and arbitrary related maps can be sensitive or bulky. Smart-inventory host ownership may differ from the selected inventory. |
| `workflow_jobs/{id}/workflow_nodes/` | Existing child-job identity/state/reason; node details add created/modified/owner metadata, `workflow_job`, `unified_job_template`, `do_not_run`, `all_parents_must_converge`, `success_nodes`, `failure_nodes`, and `always_nodes`. | Arbitrary launch variables and related maps are excluded. The reviewed list serializer already returns these detail fields, so no extra node request is necessary. |
| `me/`, `unified_jobs/?status=running&page_size=1` | Existing username and filtered count in the header. | Personal/profile/admin fields and individual running-job results are unnecessary for the header. |

Common job state includes `launch_type`, `status`, `failed`, `started`, `finished`,
`canceled_on`, `elapsed`, `job_explanation`, `execution_node`, `controller_node`,
`event_processing_finished`, and the unified template reference where exposed.
`summary_fields.source_workflow_job` and `ancestor_job` identify parent execution
context. `job_args`, `job_cwd`, `job_env`, `result_traceback`, `work_unit_id` and
arbitrary `launched_by` payloads are omitted as sensitive or low-level process
internals; owner/template/workflow references supply the relevant context.

All object endpoints also return `url`, `related`, and `summary_fields` as
applicable. Only selected summary fields are decoded for display; validated
related output/cancel/children links retain their existing behavior. Raw payloads,
credential inputs, role maps, and launch secrets are never rendered as object
details. Output and cancellation endpoints retain their existing reviewed
contracts below and are not expanded into object-field views.

Inventory and group detail reads run asynchronously when entering their screens,
which retain Groups/Hosts menus and add Tab focus for detail scrolling. Hosts open
a new detail screen. Workflow children use `d` for node details and retain Enter
for child-job navigation. Existing request generations, cancellation, and back
frames preserve context and reject stale responses. Tests cover both prefixes,
optional fields, false/zero, missing summaries, token/terminal sanitization,
repository URL redaction, safe errors, canceled/stale reads, scrolling, and
existing template/output/workflow/cancellation behavior.

## Output and completion

Request `stdout/?format=json&start_line=N&end_line=M`. Ranges are zero-based,
end-exclusive Python line slices. JSON contains raw (possibly ANSI-bearing) text
in `content`, plus `range.start`, `range.end`, `range.absolute_end`. No base64
encoding is requested. JSON is not HTML. HTTP byte ranges are not assumed.
The final unterminated line can change; reread one overlapping line at the cursor.
Store validated data before advancing. Older ranges are independently fetched.

The upstream display byte limit applies even to bounded line requests. Its JSON
fallback is a one-line explanatory message, not actual output. Recognize that
message and explicitly report the server display limit. Download fallback is
not implemented: it would require a separately verified streaming/indexing path.
404/410 indicates unavailable/expired history; unexpected ranges indicate gaps
or resets and must be surfaced.

Terminal job status alone is insufficient. Details may expose
`event_processing_finished` (absent in list responses). When true for a terminal
job, continue fetching until the accepted cursor reaches `absolute_end`. If the
field is absent, continue polling and show that final-output evidence is unknown;
do not guess completeness from a quiet interval.

System jobs in the reviewed upstream source expose inline `result_stdout` on
details rather than a stdout link. Slice that bounded text locally; reject inline
text exceeding 256 KiB explicitly. Ranged output remains preferred where exposed.

## Security and offline acceptance

HTTP and HTTPS origins are supported. HTTPS uses system trust by default;
explicit TLS verification disable is visible. HTTP connections are identified
in the banner, with TLS verification shown as not applicable.
Only same-origin, normalized paths in the selected controller prefix are allowed;
collection cursors must remain on the original collection. Redirects are rejected.
Bodies, decoded pages, output chunks, RAM, disk, and pending messages are bounded.
Errors never display HTTP bodies, URLs, or the token. Sanitize terminal sequences
and redact the known token in all server text before presentation or caching.

Each implementation milestone must pass formatting, package tests and vet before
the next. Output/lifecycle changes also require race tests. Real server testing
remains deferred, as agreed in PLAN.md.
