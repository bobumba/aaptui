package aap

import (
	"context"
	"fmt"
)

type WorkflowNode struct {
	ID               int
	Identifier, Name string
	Status           JobStatus
	Job              *JobRef
	Skipped          bool
	Reason           string
}
type wireNode struct {
	ID         int               `json:"id"`
	Identifier string            `json:"identifier"`
	Job        *int              `json:"job"`
	DoNotRun   bool              `json:"do_not_run"`
	Related    map[string]string `json:"related"`
	Summary    struct {
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
		n := WorkflowNode{ID: w.ID, Identifier: c.cleanLabel(w.Identifier), Skipped: w.DoNotRun, Status: "pending"}
		if w.DoNotRun {
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
		} else if !w.DoNotRun {
			n.Reason = "Node has no launched child job"
		}
		out.Items = append(out.Items, n)
	}
	return out, nil
}
