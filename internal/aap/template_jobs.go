package aap

import (
	"context"
	"fmt"
)

// TemplateJobs lists only jobs belonging to the selected playbook template.
func (c *Client) TemplateJobs(ctx context.Context, ref TemplateRef, o ListOptions) (Page[JobSummary], error) {
	if ref.ID <= 0 || ref.Type != PlaybookTemplate {
		return Page[JobSummary]{}, apiError(Malformed, "list template jobs")
	}
	p, err := readPage[wireJob](ctx, c, fmt.Sprintf("job_templates/%d/jobs/", ref.ID), o)
	if err != nil {
		return Page[JobSummary]{}, err
	}
	out := Page[JobSummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || w.Type != string(PlaybookJob) || (w.JobTemplate != nil && *w.JobTemplate != ref.ID) {
			return Page[JobSummary]{}, apiError(Malformed, "list template jobs")
		}
		out.Items = append(out.Items, c.jobSummary(w))
	}
	return out, nil
}
