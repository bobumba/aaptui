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

HTTPS with system trust by default; explicit TLS verification disable is visible.
Only same-origin, normalized paths in the selected controller prefix are allowed;
collection cursors must remain on the original collection. Redirects are rejected.
Bodies, decoded pages, output chunks, RAM, disk, and pending messages are bounded.
Errors never display HTTP bodies, URLs, or the token. Sanitize terminal sequences
and redact the known token in all server text before presentation or caching.

Each implementation milestone must pass formatting, package tests and vet before
the next. Output/lifecycle changes also require race tests. Real server testing
remains deferred, as agreed in PLAN.md.
