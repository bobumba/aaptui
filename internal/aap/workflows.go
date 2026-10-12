package aap

import (
	"context"
	"fmt"
)

type WorkflowNode struct {
	Metadata                                ResourceMetadata
	WorkflowJob, Template                   *ResourceReference
	SuccessNodes, FailureNodes, AlwaysNodes []int
	AllParentsMustConverge, DoNotRun        *bool
	ID                                      int
	Identifier, Name                        string
	Status                                  JobStatus
	Job                                     *JobRef
	Skipped                                 bool
	Reason                                  string
}
type wireNode struct {
	wireDetails
	WorkflowJob            *int              `json:"workflow_job"`
	Template               *int              `json:"unified_job_template"`
	SuccessNodes           []int             `json:"success_nodes"`
	FailureNodes           []int             `json:"failure_nodes"`
	AlwaysNodes            []int             `json:"always_nodes"`
	AllParentsMustConverge *bool             `json:"all_parents_must_converge"`
	ID                     int               `json:"id"`
	Identifier             string            `json:"identifier"`
	Job                    *int              `json:"job"`
	DoNotRun               *bool             `json:"do_not_run"`
	Related                map[string]string `json:"related"`
	Summary                struct {
		wireSummary
		Job *struct {
			ID     int    `json:"id"`
			Type   string `json:"type"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"job"`
	} `json:"summary_fields"`
}

func (c *Client) WorkflowNodes(ctx context.Context, ref JobRef, o ListOptions) (Page[WorkflowNode], error) {
	if ref.Type != WorkflowJob || ref.ID <= 0 {
		return Page[WorkflowNode]{}, apiError(Unsupported, "read workflow children")
	}
	route := fmt.Sprintf("workflow_jobs/%d/workflow_nodes/", ref.ID)
	p, err := readPage[wireNode](ctx, c, route, o)
	if err != nil {
		return Page[WorkflowNode]{}, err
	}
	out := Page[WorkflowNode]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 {
			return Page[WorkflowNode]{}, apiError(Malformed, "read workflow children")
		}
		w.wireDetails.Summary = w.Summary.wireSummary
		n := WorkflowNode{ID: w.ID, Identifier: c.cleanLabel(w.Identifier), Skipped: w.DoNotRun != nil && *w.DoNotRun, Status: "pending",
			Metadata: c.metadata(w.wireDetails), WorkflowJob: c.reference(w.WorkflowJob, nil), Template: c.reference(w.Template, w.Summary.UnifiedJobTemplate),
			SuccessNodes: w.SuccessNodes, FailureNodes: w.FailureNodes, AlwaysNodes: w.AlwaysNodes,
			AllParentsMustConverge: w.AllParentsMustConverge, DoNotRun: w.DoNotRun}
		if n.Skipped {
			n.Status = "skipped"
			n.Reason = "Node was skipped"
		}
		if w.Job != nil {
			if *w.Job <= 0 {
				return Page[WorkflowNode]{}, apiError(Malformed, "read workflow children")
			}
			n.Job = &JobRef{ID: *w.Job}
			n.Reason = "Child details may be inaccessible or deleted"
			if summary := w.Summary.Job; summary != nil {
				if summary.ID != 0 && summary.ID != *w.Job {
					return Page[WorkflowNode]{}, apiError(Malformed, "read workflow children")
				}
				if summary.Type != "" && !validType(summary.Type) {
					return Page[WorkflowNode]{}, apiError(Malformed, "read workflow children")
				}
				n.Job.Type = JobType(c.CleanText(summary.Type))
				n.Name = c.cleanLabel(summary.Name)
				n.Status = JobStatus(c.cleanLabel(summary.Status))
				n.Reason = ""
			}
			if link := w.Related["job"]; link != "" {
				u, err := c.safeLink(link)
				if err != nil {
					return Page[WorkflowNode]{}, err
				}
				if n.Job.Type != "" && u.Path != c.endpoint(fmt.Sprintf("%s/%d/", jobCollection(n.Job.Type), n.Job.ID)) {
					return Page[WorkflowNode]{}, apiError(Malformed, "read workflow children")
				}
			}
		} else if !n.Skipped {
			n.Reason = "Node has no launched child job"
		}
		out.Items = append(out.Items, n)
	}
	return out, nil
}
