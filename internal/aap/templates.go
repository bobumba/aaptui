package aap

import (
	"context"
	"fmt"
	"net/http"
)

type TemplateType string

const (
	PlaybookTemplate  TemplateType = "job_template"
	WorkflowTemplate  TemplateType = "workflow_job_template"
	ProjectTemplate   TemplateType = "project"
	InventoryTemplate TemplateType = "inventory_source"
	SystemTemplate    TemplateType = "system_job_template"
	ApprovalTemplate  TemplateType = "workflow_approval_template"
)

type TemplateRef struct {
	ID   int
	Type TemplateType
}
type TemplateSummary struct {
	Ref                       TemplateRef
	Name, Description, Status string
}
type PlaybookSettings struct {
	Playbook, JobType, Limit string
	Inventory, Project       *int
}
type ProjectSettings struct{ SCMType, SCMURL, SCMBranch string }
type InventorySettings struct {
	Source, SourcePath string
	Inventory          *int
}
type TemplateDetails struct {
	TemplateSummary
	Playbook        *PlaybookSettings
	Project         *ProjectSettings
	Inventory       *InventorySettings
	SystemJobType   string
	ApprovalTimeout *int
}
type wireResource struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Playbook    string `json:"playbook"`
	JobType     string `json:"job_type"`
	Limit       string `json:"limit"`
	Inventory   *int   `json:"inventory"`
	Project     *int   `json:"project"`
	SCMType     string `json:"scm_type"`
	SCMURL      string `json:"scm_url"`
	SCMBranch   string `json:"scm_branch"`
	Source      string `json:"source"`
	SourcePath  string `json:"source_path"`
	Timeout     *int   `json:"timeout"`
}

func (c *Client) templateSummary(w wireResource) TemplateSummary {
	return TemplateSummary{Ref: TemplateRef{w.ID, TemplateType(c.CleanText(w.Type))}, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Status: c.cleanLabel(w.Status)}
}
func (c *Client) ListTemplates(ctx context.Context, o ListOptions) (Page[TemplateSummary], error) {
	p, err := readPage[wireResource](ctx, c, "job_templates/", o)
	if err != nil {
		return Page[TemplateSummary]{}, err
	}
	out := Page[TemplateSummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || !validType(w.Type) {
			return Page[TemplateSummary]{}, apiError(Malformed, "list templates")
		}
		out.Items = append(out.Items, c.templateSummary(w))
	}
	return out, nil
}
func templateCollection(t TemplateType) string {
	switch t {
	case PlaybookTemplate:
		return "job_templates"
	case WorkflowTemplate:
		return "workflow_job_templates"
	case ProjectTemplate:
		return "projects"
	case InventoryTemplate:
		return "inventory_sources"
	case SystemTemplate:
		return "system_job_templates"
	case ApprovalTemplate:
		return "workflow_approval_templates"
	default:
		return "unified_job_templates"
	}
}
func (c *Client) Template(ctx context.Context, ref TemplateRef) (TemplateDetails, error) {
	if ref.ID <= 0 {
		return TemplateDetails{}, apiError(Malformed, "read template")
	}
	var w wireResource
	if err := c.request(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("%s/%d/", templateCollection(ref.Type), ref.ID)), "read template", &w); err != nil {
		return TemplateDetails{}, err
	}
	if w.ID != ref.ID || !validType(w.Type) || TemplateType(c.CleanText(w.Type)) != ref.Type {
		return TemplateDetails{}, apiError(Malformed, "read template")
	}
	d := TemplateDetails{TemplateSummary: c.templateSummary(w)}
	switch ref.Type {
	case PlaybookTemplate:
		d.Playbook = &PlaybookSettings{c.CleanText(w.Playbook), c.CleanText(w.JobType), c.CleanText(w.Limit), w.Inventory, w.Project}
	case ProjectTemplate:
		d.Project = &ProjectSettings{c.CleanText(w.SCMType), c.CleanText(w.SCMURL), c.CleanText(w.SCMBranch)}
	case InventoryTemplate:
		d.Inventory = &InventorySettings{c.CleanText(w.Source), c.CleanText(w.SourcePath), w.Inventory}
	case SystemTemplate:
		d.SystemJobType = c.CleanText(w.JobType)
	case ApprovalTemplate:
		d.ApprovalTimeout = w.Timeout
	}
	return d, nil
}
