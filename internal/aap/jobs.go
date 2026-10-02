package aap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type JobType string

const (
	PlaybookJob      JobType = "job"
	WorkflowJob      JobType = "workflow_job"
	ProjectUpdate    JobType = "project_update"
	InventoryUpdate  JobType = "inventory_update"
	AdHocCommand     JobType = "ad_hoc_command"
	SystemJob        JobType = "system_job"
	WorkflowApproval JobType = "workflow_approval"
)

type JobStatus string

func (s JobStatus) Active() bool {
	switch s {
	case "new", "pending", "waiting", "running":
		return true
	}
	return false
}
func (s JobStatus) Terminal() bool {
	switch s {
	case "successful", "failed", "error", "canceled":
		return true
	}
	return false
}

type JobRef struct {
	ID   int
	Type JobType
}
type JobSummary struct {
	Ref               JobRef
	Name, Description string
	Status            JobStatus
	Started, Finished string
	Elapsed           float64
}
type CapabilityState string

const (
	Available   CapabilityState = "available"
	Unavailable CapabilityState = "unavailable"
	Unknown     CapabilityState = "unknown"
)

type Capability struct {
	State  CapabilityState
	Reason string
}
type JobCapabilities struct{ Output, Cancel, Children Capability }
type JobDetails struct {
	systemText *string
	JobSummary
	Capabilities                                       JobCapabilities
	EventProcessingFinished                            *bool
	Playbook                                           *PlaybookSettings
	Project                                            *ProjectSettings
	Inventory                                          *InventorySettings
	ModuleName, ModuleArgs, SystemJobType, Explanation string
	ApprovalTimeout                                    *int
	TimedOut                                           *bool
	stdout, cancel, nodes                              string
}
type wireJob struct {
	ResultStdout *string `json:"result_stdout"`
	wireResource
	Started                 string            `json:"started"`
	Finished                string            `json:"finished"`
	Elapsed                 float64           `json:"elapsed"`
	EventProcessingFinished *bool             `json:"event_processing_finished"`
	ModuleName              string            `json:"module_name"`
	ModuleArgs              string            `json:"module_args"`
	Explanation             string            `json:"job_explanation"`
	TimedOut                *bool             `json:"timed_out"`
	Related                 map[string]string `json:"related"`
}

func jobCollection(t JobType) string {
	switch t {
	case PlaybookJob:
		return "jobs"
	case WorkflowJob:
		return "workflow_jobs"
	case ProjectUpdate:
		return "project_updates"
	case InventoryUpdate:
		return "inventory_updates"
	case AdHocCommand:
		return "ad_hoc_commands"
	case SystemJob:
		return "system_jobs"
	case WorkflowApproval:
		return "workflow_approvals"
	default:
		return "unified_jobs"
	}
}
func (c *Client) jobSummary(w wireJob) JobSummary {
	return JobSummary{Ref: JobRef{w.ID, JobType(c.CleanText(w.Type))}, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Status: JobStatus(c.cleanLabel(w.Status)), Started: c.cleanLabel(w.Started), Finished: c.cleanLabel(w.Finished), Elapsed: w.Elapsed}
}
func (c *Client) ListJobs(ctx context.Context, o ListOptions) (Page[JobSummary], error) {
	p, err := readPage[wireJob](ctx, c, "unified_jobs/", o)
	if err != nil {
		return Page[JobSummary]{}, err
	}
	out := Page[JobSummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || !validType(w.Type) {
			return Page[JobSummary]{}, apiError(Malformed, "list jobs")
		}
		out.Items = append(out.Items, c.jobSummary(w))
	}
	return out, nil
}
func (c *Client) Job(ctx context.Context, ref JobRef) (JobDetails, error) {
	if ref.ID <= 0 {
		return JobDetails{}, apiError(Malformed, "read job")
	}
	var w wireJob
	route := fmt.Sprintf("%s/%d/", jobCollection(ref.Type), ref.ID)
	if err := c.request(ctx, http.MethodGet, c.endpoint(route), "read job", &w); err != nil {
		return JobDetails{}, err
	}
	if w.ID != ref.ID || !validType(w.Type) || (ref.Type != "" && JobType(w.Type) != ref.Type) {
		return JobDetails{}, apiError(Malformed, "read job")
	}
	d := JobDetails{JobSummary: c.jobSummary(w), EventProcessingFinished: w.EventProcessingFinished, Explanation: c.CleanText(w.Explanation)}
	unknown := Capability{Unknown, "Capability not exposed by this resource"}
	d.Capabilities = JobCapabilities{unknown, unknown, unknown}
	d.stdout = w.Related["stdout"]
	d.cancel = w.Related["cancel"]
	d.nodes = w.Related["workflow_nodes"]
	// Related operations must belong to this exact resource, not merely its origin.
	for _, pair := range []struct{ link, suffix string }{{d.stdout, "stdout/"}, {d.cancel, "cancel/"}, {d.nodes, "workflow_nodes/"}} {
		if pair.link == "" {
			continue
		}
		u, err := c.safeLink(pair.link)
		if err != nil || u.Path != c.endpoint(route+pair.suffix) {
			return JobDetails{}, apiError(Malformed, "read job capabilities")
		}
	}
	switch d.Ref.Type {
	case PlaybookJob:
		d.Playbook = &PlaybookSettings{c.CleanText(w.Playbook), c.CleanText(w.JobType), c.CleanText(w.Limit), w.Inventory, w.Project}
	case ProjectUpdate:
		d.Project = &ProjectSettings{c.CleanText(w.SCMType), c.CleanText(w.SCMURL), c.CleanText(w.SCMBranch)}
	case InventoryUpdate:
		d.Inventory = &InventorySettings{c.CleanText(w.Source), c.CleanText(w.SourcePath), w.Inventory}
	case AdHocCommand:
		d.ModuleName = c.CleanText(w.ModuleName)
		d.ModuleArgs = c.CleanText(w.ModuleArgs)
	case SystemJob:
		d.systemText = w.ResultStdout
		d.SystemJobType = c.CleanText(w.JobType)
	case WorkflowApproval:
		d.ApprovalTimeout = w.Timeout
		d.TimedOut = w.TimedOut
	}
	known := jobCollection(d.Ref.Type) != "unified_jobs"
	if known {
		d.Capabilities.Children = Capability{Unavailable, "This job has no workflow children"}
		d.Capabilities.Output = Capability{Unavailable, "This resource does not expose standard output"}
		d.Capabilities.Cancel = Capability{Unavailable, "Cancellation is not exposed"}
	}
	if d.Ref.Type == SystemJob && d.systemText != nil {
		d.Capabilities.Output = Capability{Available, "Inline stdout; bounded client-side ranges"}
	}
	if d.stdout != "" && d.Ref.Type != WorkflowJob && d.Ref.Type != WorkflowApproval {
		d.Capabilities.Output = Capability{Available, ""}
	}
	if d.Ref.Type == WorkflowJob && d.nodes != "" {
		d.Capabilities.Children = Capability{Available, ""}
	}
	if d.Ref.Type != WorkflowApproval && d.cancel != "" {
		if !d.Status.Active() {
			d.Capabilities.Cancel = Capability{Unavailable, "Job is not active"}
		} else {
			var result struct {
				CanCancel *bool `json:"can_cancel"`
			}
			err := c.request(ctx, http.MethodGet, d.cancel, "check cancellation", &result)
			var api *APIError
			if err != nil {
				if errors.As(err, &api) && (api.Kind == Permission || api.Kind == Unsupported || api.Kind == Missing) {
					d.Capabilities.Cancel = Capability{Unavailable, api.Error()}
				} else if errors.As(err, &api) && api.Kind != Authentication {
					d.Capabilities.Cancel = Capability{Unknown, api.Error()}
				} else {
					return JobDetails{}, err
				}
			} else if result.CanCancel == nil {
				d.Capabilities.Cancel = Capability{Unknown, "Cancellation capability was not returned"}
			} else if *result.CanCancel {
				d.Capabilities.Cancel = Capability{Available, ""}
			} else {
				d.Capabilities.Cancel = Capability{Unavailable, "Server reports that the job cannot be cancelled"}
			}
		}
	}
	return d, nil
}
