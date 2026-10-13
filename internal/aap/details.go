package aap

import "net/url"

// ResourceReference uses names already returned in summary_fields. An ID remains
// useful when the token cannot see the related resource's summary.
type ResourceReference struct {
	ID   int
	Name string
}

type ResourceMetadata struct {
	Created, Modified                   string
	Organization, CreatedBy, ModifiedBy *ResourceReference
}

type ExecutionSettings struct {
	ResolvedEnvironment                                       *ResourceReference
	Credential                                                *ResourceReference
	Credentials                                               []ResourceReference
	Inventory, Project, Environment                           *ResourceReference
	JobType, Limit, SCMBranch, JobTags, SkipTags, StartAtTask string
	Forks, Verbosity, Timeout, JobSliceCount                  *int
	BecomeEnabled, DiffMode, ForceHandlers, UseFactCache      *bool
	AllowSimultaneous, SurveyEnabled                          *bool
}

type wireReference struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type wireSummary struct {
	UserCapabilities struct {
		Start *bool `json:"start"`
	} `json:"user_capabilities"`
	ResolvedEnvironment *wireReference  `json:"resolved_environment"`
	SourceWorkflowJob   *wireReference  `json:"source_workflow_job"`
	AncestorJob         *wireReference  `json:"ancestor_job"`
	Credential          *wireReference  `json:"credential"`
	Credentials         []wireReference `json:"credentials"`
	DefaultEnvironment  *wireReference  `json:"default_environment"`
	Organization        *wireReference  `json:"organization"`
	CreatedBy           *wireReference  `json:"created_by"`
	ModifiedBy          *wireReference  `json:"modified_by"`
	Inventory           *wireReference  `json:"inventory"`
	Project             *wireReference  `json:"project"`
	Environment         *wireReference  `json:"execution_environment"`
	UnifiedJobTemplate  *wireReference  `json:"unified_job_template"`
	SourceProject       *wireReference  `json:"source_project"`
	InventorySource     *wireReference  `json:"inventory_source"`
}

// wireDetails contains only selected fields. Arbitrary variables, environments,
// artifacts, credentials and launch secrets are deliberately not decoded.
type wireDetails struct {
	Credential            *int        `json:"credential"`
	DefaultEnvironment    *int        `json:"default_environment"`
	Created               string      `json:"created"`
	Modified              string      `json:"modified"`
	Organization          *int        `json:"organization"`
	Environment           *int        `json:"execution_environment"`
	Summary               wireSummary `json:"summary_fields"`
	Forks                 *int        `json:"forks"`
	Verbosity             *int        `json:"verbosity"`
	JobTags               string      `json:"job_tags"`
	SkipTags              string      `json:"skip_tags"`
	StartAtTask           string      `json:"start_at_task"`
	JobSliceCount         *int        `json:"job_slice_count"`
	BecomeEnabled         *bool       `json:"become_enabled"`
	DiffMode              *bool       `json:"diff_mode"`
	ForceHandlers         *bool       `json:"force_handlers"`
	UseFactCache          *bool       `json:"use_fact_cache"`
	AllowSimultaneous     *bool       `json:"allow_simultaneous"`
	SurveyEnabled         *bool       `json:"survey_enabled"`
	LastJobRun            string      `json:"last_job_run"`
	NextJobRun            string      `json:"next_job_run"`
	LastJobFailed         *bool       `json:"last_job_failed"`
	SCMRevision           string      `json:"scm_revision"`
	SCMRefspec            string      `json:"scm_refspec"`
	LocalPath             string      `json:"local_path"`
	SCMClean              *bool       `json:"scm_clean"`
	SCMTrackSubmodules    *bool       `json:"scm_track_submodules"`
	SCMDeleteOnUpdate     *bool       `json:"scm_delete_on_update"`
	SCMUpdateOnLaunch     *bool       `json:"scm_update_on_launch"`
	SCMUpdateCacheTimeout *int        `json:"scm_update_cache_timeout"`
	AllowOverride         *bool       `json:"allow_override"`
	SourceProject         *int        `json:"source_project"`
	InventorySource       *int        `json:"inventory_source"`
	HostFilter            string      `json:"host_filter"`
	Overwrite             *bool       `json:"overwrite"`
	OverwriteVars         *bool       `json:"overwrite_vars"`
	UpdateOnLaunch        *bool       `json:"update_on_launch"`
	UpdateCacheTimeout    *int        `json:"update_cache_timeout"`
}

func (c *Client) reference(id *int, summary *wireReference) *ResourceReference {
	if id == nil && summary != nil {
		id = &summary.ID
	}
	if id == nil || *id <= 0 {
		return nil
	}
	ref := &ResourceReference{ID: *id}
	if summary != nil && (summary.ID == 0 || summary.ID == *id) {
		name := summary.Name
		if name == "" {
			name = summary.Username
		}
		ref.Name = c.cleanLabel(name)
	}
	return ref
}

func (c *Client) metadata(w wireDetails) ResourceMetadata {
	return ResourceMetadata{
		Created: c.cleanLabel(w.Created), Modified: c.cleanLabel(w.Modified),
		Organization: c.reference(w.Organization, w.Summary.Organization),
		CreatedBy:    c.reference(nil, w.Summary.CreatedBy), ModifiedBy: c.reference(nil, w.Summary.ModifiedBy),
	}
}

func (c *Client) executionSettings(w wireResource) ExecutionSettings {
	settings := ExecutionSettings{
		ResolvedEnvironment: c.reference(nil, w.Summary.ResolvedEnvironment),
		Credential:          c.reference(w.Credential, w.Summary.Credential),
		Inventory:           c.reference(w.Inventory, w.Summary.Inventory), Project: c.reference(w.Project, w.Summary.Project),
		Environment: c.reference(w.Environment, w.Summary.Environment), JobType: c.cleanLabel(w.JobType),
		Limit: c.cleanLabel(w.Limit), SCMBranch: c.cleanLabel(w.SCMBranch), JobTags: c.cleanLabel(w.JobTags),
		SkipTags: c.cleanLabel(w.SkipTags), StartAtTask: c.cleanLabel(w.StartAtTask),
		Forks: w.Forks, Verbosity: w.Verbosity, Timeout: w.Timeout, JobSliceCount: w.JobSliceCount,
		BecomeEnabled: w.BecomeEnabled, DiffMode: w.DiffMode, ForceHandlers: w.ForceHandlers, UseFactCache: w.UseFactCache,
		AllowSimultaneous: w.AllowSimultaneous, SurveyEnabled: w.SurveyEnabled,
	}
	for _, credential := range w.Summary.Credentials {
		if ref := c.reference(nil, &credential); ref != nil {
			if settings.Credential != nil && settings.Credential.ID == ref.ID {
				continue
			}
			settings.Credentials = append(settings.Credentials, *ref)
		}
	}
	return settings
}

func (c *Client) projectSettings(w wireResource) *ProjectSettings {
	// SCM URLs can embed passwords or query tokens. Display repository location
	// without userinfo, query parameters or fragments.
	scmURL := w.SCMURL
	if u, err := url.Parse(scmURL); err == nil && u.Scheme != "" && u.Host != "" {
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		scmURL = u.String()
	} else if err != nil {
		scmURL = "Unavailable"
	}
	return &ProjectSettings{
		DefaultEnvironment: c.reference(w.DefaultEnvironment, w.Summary.DefaultEnvironment),
		SCMType:            c.cleanLabel(w.SCMType), SCMURL: c.cleanLabel(scmURL), SCMBranch: c.cleanLabel(w.SCMBranch),
		SCMRevision: c.cleanLabel(w.SCMRevision), SCMRefspec: c.cleanLabel(w.SCMRefspec), LocalPath: c.cleanLabel(w.LocalPath),
		Clean: w.SCMClean, TrackSubmodules: w.SCMTrackSubmodules, DeleteOnUpdate: w.SCMDeleteOnUpdate,
		UpdateOnLaunch: w.SCMUpdateOnLaunch, UpdateCacheTimeout: w.SCMUpdateCacheTimeout, AllowOverride: w.AllowOverride,
	}
}

func (c *Client) inventorySettings(w wireResource) *InventorySettings {
	return &InventorySettings{
		Source: c.cleanLabel(w.Source), SourcePath: c.cleanLabel(w.SourcePath), Inventory: w.Inventory,
		SourceProject:   c.reference(w.SourceProject, w.Summary.SourceProject),
		InventorySource: c.reference(w.InventorySource, w.Summary.InventorySource),
		SCMRevision:     c.cleanLabel(w.SCMRevision), HostFilter: c.cleanLabel(w.HostFilter),
		Overwrite: w.Overwrite, OverwriteVars: w.OverwriteVars, UpdateOnLaunch: w.UpdateOnLaunch, UpdateCacheTimeout: w.UpdateCacheTimeout,
	}
}
