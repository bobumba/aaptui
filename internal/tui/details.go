package tui

import (
	"fmt"
	"strconv"
	"strings"

	"aaptui/internal/aap"
)

type detailField struct {
	label, value string
}

// detailFields keeps data separate from styling and optional values distinct
// from false and zero. An empty label denotes a section heading.
type detailFields []detailField

func (f *detailFields) field(label, value string) {
	if value == "" {
		value = "—"
	}
	*f = append(*f, detailField{label: label, value: value})
}
func (f *detailFields) section(title string) {
	*f = append(*f, detailField{value: title})
}
func (f *detailFields) identity(id int, name, kind, description string) {
	f.field("ID", strconv.Itoa(id))
	f.field("Name", name)
	f.field("Type", kind)
	f.text("Description", description)
}
func (f *detailFields) text(label, value string) {
	if value != "" {
		f.field(label, value)
	}
}
func (f *detailFields) flag(label string, value *bool) {
	if value != nil {
		f.field(label, strconv.FormatBool(*value))
	}
}
func (f *detailFields) number(label string, value *int) {
	if value != nil {
		f.field(label, strconv.Itoa(*value))
	}
}
func (f *detailFields) reference(label string, value *aap.ResourceReference) {
	if value != nil {
		text := strconv.Itoa(value.ID)
		if value.Name != "" {
			text += " · " + value.Name
		}
		f.field(label, text)
	}
}
func (f *detailFields) metadata(d aap.ResourceMetadata) {
	f.field("Created", jobTimestamp(d.Created))
	f.field("Modified", jobTimestamp(d.Modified))
	f.reference("Organization", d.Organization)
	f.reference("Created by", d.CreatedBy)
	f.reference("Modified by", d.ModifiedBy)
}
func (f *detailFields) execution(d aap.ExecutionSettings) {
	f.reference("Credential", d.Credential)
	for _, credential := range d.Credentials {
		f.reference("Credential", &credential)
	}
	f.reference("Inventory", d.Inventory)
	f.reference("Project", d.Project)
	f.reference("Execution environment", d.Environment)
	f.reference("Resolved execution environment", d.ResolvedEnvironment)
	f.text("Job type", d.JobType)
	f.text("Limit", d.Limit)
	f.text("SCM branch", d.SCMBranch)
	f.text("Job tags", d.JobTags)
	f.text("Skip tags", d.SkipTags)
	f.text("Start at task", d.StartAtTask)
	f.number("Forks", d.Forks)
	f.number("Verbosity", d.Verbosity)
	f.number("Timeout (seconds)", d.Timeout)
	f.number("Job slice count", d.JobSliceCount)
	f.flag("Privilege escalation", d.BecomeEnabled)
	f.flag("Diff mode", d.DiffMode)
	f.flag("Force handlers", d.ForceHandlers)
	f.flag("Fact cache", d.UseFactCache)
	f.flag("Allow simultaneous", d.AllowSimultaneous)
	f.flag("Survey enabled", d.SurveyEnabled)
}
func (f *detailFields) project(d *aap.ProjectSettings) {
	if d == nil {
		return
	}
	f.reference("Default execution environment", d.DefaultEnvironment)
	f.text("SCM", d.SCMType)
	f.text("URL", d.SCMURL)
	f.text("Branch", d.SCMBranch)
	f.text("SCM revision", d.SCMRevision)
	f.text("SCM refspec", d.SCMRefspec)
	f.text("Local path", d.LocalPath)
	f.flag("SCM clean", d.Clean)
	f.flag("Track submodules", d.TrackSubmodules)
	f.flag("Delete on update", d.DeleteOnUpdate)
	f.flag("SCM update on launch", d.UpdateOnLaunch)
	f.number("SCM update cache timeout (seconds)", d.UpdateCacheTimeout)
	f.flag("Allow branch override", d.AllowOverride)
}
func (f *detailFields) source(d *aap.InventorySettings) {
	if d == nil {
		return
	}
	f.field("Source", d.Source)
	f.text("Path", d.SourcePath)
	f.reference("Source project", d.SourceProject)
	f.reference("Inventory source", d.InventorySource)
	f.text("SCM revision", d.SCMRevision)
	f.text("Host filter", d.HostFilter)
	f.flag("Overwrite hosts/groups", d.Overwrite)
	f.flag("Overwrite variables", d.OverwriteVars)
	f.flag("Update on launch", d.UpdateOnLaunch)
	f.number("Update cache timeout (seconds)", d.UpdateCacheTimeout)
}

func (m *Model) details(width int) []string {
	var fields detailFields
	fields.section("Details")
	switch m.screen {
	case templateScreen:
		d := m.templateDetail
		fields.identity(d.Ref.ID, d.Name, string(d.Ref.Type), d.Description)
		if d.Ref.Type == aap.PlaybookTemplate && m.launcher != nil {
			state := d.Launch.State
			if state == "" {
				state = aap.Unknown
			}
			fields.field("Launch (L)", string(state)+" · "+d.Launch.Reason)
		}
		fields.field("Status", d.Status)
		if d.Playbook != nil {
			fields.field("Playbook", d.Playbook.Playbook)
		}
		fields.metadata(d.Metadata)
		fields.field("Last job run", jobTimestamp(d.LastJobRun))
		fields.flag("Last job failed", d.LastJobFailed)
		fields.field("Next job run", jobTimestamp(d.NextJobRun))
		fields.section("Configuration")

		fields.execution(d.Options)
		fields.project(d.Project)
		fields.source(d.Inventory)
		fields.text("System job", d.SystemJobType)
		fields.number("Approval timeout (seconds)", d.ApprovalTimeout)
	case inventoryScreen:
		d := m.inventoryDetail
		fields.identity(d.ID, d.Name, "inventory", d.Description)
		fields.field("Kind", d.Kind)
		fields.metadata(d.Metadata)
		fields.text("Host filter", d.HostFilter)
		fields.section("Inventory state")
		fields.number("Total hosts", d.TotalHosts)
		fields.number("Total groups", d.TotalGroups)
		fields.number("Total inventory sources", d.TotalInventorySources)
		fields.number("Hosts with active failures", d.HostsWithActiveFailures)
		fields.number("Inventory sources with failures", d.InventorySourcesWithFailures)
		fields.flag("Active failures", d.HasActiveFailures)
		fields.flag("Has inventory sources", d.HasInventorySources)
		fields.flag("Pending deletion", d.PendingDeletion)
		fields.flag("Prevent instance group fallback", d.PreventInstanceGroupFallback)
	case groupScreen:
		d := m.groupDetail
		fields.identity(d.ID, d.Name, "group", d.Description)
		fields.metadata(d.Metadata)
		fields.reference("Inventory", d.Inventory)
	case hostScreen:
		d := m.hostDetail
		fields.identity(d.ID, d.Name, "host", d.Description)
		fields.metadata(d.Metadata)
		fields.reference("Inventory", d.Inventory)
		if d.Enabled == nil {
			fields.field("Enabled", "Unavailable")
		} else {
			fields.flag("Enabled", d.Enabled)
		}
		fields.text("Instance ID", d.InstanceID)
		fields.flag("Active failures", d.HasActiveFailures)
		fields.flag("Has inventory sources", d.HasInventorySources)
		fields.number("Last job ID", d.LastJob)
		fields.number("Last job host summary ID", d.LastJobHostSummary)
		fields.field("Facts modified", jobTimestamp(d.FactsModified))
		fields = append(fields, m.hostGroupFields()...)
	case nodeScreen:
		d := m.nodeDetail
		fields.field("ID", strconv.Itoa(d.ID))
		fields.field("Identifier", d.Identifier)
		fields.field("Type", "workflow node")
		fields.field("Status", string(d.Status))
		fields.text("Child name", d.Name)
		fields.metadata(d.Metadata)
		fields.reference("Workflow job", d.WorkflowJob)
		fields.reference("Template", d.Template)
		if d.Job != nil {
			fields.field("Child job", fmt.Sprintf("%d · %s", d.Job.ID, d.Job.Type))
		}
		fields.text("Reason", d.Reason)
		fields.flag("Do not run", d.DoNotRun)
		fields.flag("All parents must converge", d.AllParentsMustConverge)
		for _, edges := range []struct {
			label string
			ids   []int
		}{{"Success nodes", d.SuccessNodes}, {"Failure nodes", d.FailureNodes}, {"Always nodes", d.AlwaysNodes}} {
			if edges.ids != nil {
				var ids []string
				for _, id := range edges.ids {
					ids = append(ids, strconv.Itoa(id))
				}
				value := strings.Join(ids, ", ")
				if value == "" {
					value = "None"
				}
				fields.field(edges.label, value)
			}
		}
	default:
		d := m.jobDetail
		fields[0].value = "Job details"
		fields.identity(d.Ref.ID, d.Name, string(d.Ref.Type), d.Description)
		fields.field("Status", string(d.Status))
		fields.field("Elapsed (seconds)", fmt.Sprintf("%.1f", d.Elapsed))
		fields.field("Started", jobTimestamp(d.Started))
		fields.field("Finished", jobTimestamp(d.Finished))
		fields.flag("Failed", d.Failed)
		fields.text("Launch type", d.LaunchType)
		fields.text("Failure / status explanation", d.Explanation)
		fields.metadata(d.Metadata)
		fields.reference("Template", d.Template)
		fields.reference("Source workflow job", d.SourceWorkflowJob)
		fields.reference("Ancestor job", d.AncestorJob)
		fields.text("Execution node", d.ExecutionNode)
		fields.text("Controller node", d.ControllerNode)
		if d.CanceledOn != "" {
			fields.field("Canceled on", jobTimestamp(d.CanceledOn))
		}
		if d.Playbook != nil {
			fields.field("Playbook", d.Playbook.Playbook)
			fields.text("SCM revision", d.SCMRevision)
		}
		fields.execution(d.Options)
		fields.project(d.Project)
		fields.source(d.Inventory)
		fields.text("Module", d.ModuleName)
		fields.text("System job", d.SystemJobType)
		fields.number("Job slice number", d.JobSliceNumber)
		fields.flag("License error", d.LicenseError)
		fields.flag("Organization host limit error", d.OrgHostLimitError)
		fields.number("Approval timeout (seconds)", d.ApprovalTimeout)
		if d.ApprovalExpiration != "" {
			fields.field("Approval expiration", jobTimestamp(d.ApprovalExpiration))
		}
		fields.flag("Approval timed out", d.TimedOut)
		fields.flag("Event processing finished", d.EventProcessingFinished)
		fields.section("Survey / Extra variables")
		switch {
		case d.ExtraVars == nil:
			fields.field("Variables", "Not supported for this job type")
		case d.ExtraVars.UnavailableReason != "":
			fields.field("Variables", "Unavailable · "+d.ExtraVars.UnavailableReason)
		case len(d.ExtraVars.Values) == 0:
			fields.field("Variables", "No recorded variables")
		default:
			for _, variable := range d.ExtraVars.Values {
				fields.field(variable.Name, variable.Value)
			}
		}
	}
	if m.screen == jobScreen {
		fields = append(m.capabilityFields(), fields...)
	}
	return m.renderDetailFields(fields, width)
}

func (m *Model) capabilityFields() detailFields {
	var fields detailFields
	fields.section("Capabilities")
	for _, item := range []struct {
		label string
		value aap.Capability
	}{
		{"Output (o)", m.jobDetail.Capabilities.Output},
		{"Children (w)", m.jobDetail.Capabilities.Children},
		{"Cancel (c)", m.jobDetail.Capabilities.Cancel},
		{"Relaunch (R)", m.jobDetail.Capabilities.Relaunch},
	} {
		state := item.value.State
		if state == "" {
			state = aap.Unknown
		}
		value := string(state)
		if item.value.Reason != "" {
			value += " · " + item.value.Reason
		}
		fields.field(item.label, value)
	}
	return fields
}
