package aap

import (
	"context"
	"errors"
	"net/http"
)

type RelaunchResult struct {
	Source, Job          JobDetails
	Requested, Ambiguous bool
}

func (c *Client) relaunchCapability(ctx context.Context, d JobDetails) (Capability, error) {
	switch d.Ref.Type {
	case PlaybookJob, WorkflowJob, AdHocCommand:
	default:
		return Capability{Unavailable, "Relaunch is not supported for this job type"}, nil
	}
	if d.relaunch == "" {
		return Capability{Unavailable, "Relaunch is not exposed"}, nil
	}
	if d.startPermission == nil {
		return Capability{Unknown, "Relaunch permission was not returned"}, nil
	}
	if !*d.startPermission {
		return Capability{Unavailable, "Token does not have permission to relaunch this job"}, nil
	}
	var requirements struct {
		Passwords *[]string `json:"passwords_needed_to_start"`
	}
	err := c.request(ctx, http.MethodGet, d.relaunch, "check relaunch requirements", &requirements)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) {
			switch api.Kind {
			case Permission, Missing, Unsupported:
				return Capability{Unavailable, api.Error()}, nil
			case Authentication:
				return Capability{}, err
			default:
				return Capability{Unknown, api.Error()}, nil
			}
		}
		return Capability{}, err
	}
	if d.Ref.Type != WorkflowJob && requirements.Passwords == nil {
		return Capability{Unknown, "Relaunch requirements were not returned"}, nil
	}
	if requirements.Passwords != nil && len(*requirements.Passwords) > 0 {
		return Capability{Unavailable, "Relaunch requires credential passwords; use the controller UI"}, nil
	}
	return Capability{Available, ""}, nil
}

// Relaunch rechecks permissions and inputs and performs one POST. An uncertain
// response is never retried: another POST could create a duplicate execution.
func (c *Client) Relaunch(ctx context.Context, ref JobRef) (RelaunchResult, error) {
	d, err := c.Job(ctx, ref)
	if err != nil {
		return RelaunchResult{}, err
	}
	result := RelaunchResult{Source: d}
	if d.Capabilities.Relaunch.State != Available {
		kind := Unsupported
		if d.Capabilities.Relaunch.State == Unknown {
			kind = Malformed
		} else if d.startPermission != nil && !*d.startPermission && d.relaunch != "" {
			kind = Permission
		}
		return result, apiError(kind, "relaunch job: "+d.Capabilities.Relaunch.Reason)
	}
	var created struct {
		ID          int     `json:"id"`
		Type        JobType `json:"type"`
		LegacyJob   *int    `json:"job"`
		LegacyAdHoc *int    `json:"ad_hoc_command"`
	}
	err = c.request(ctx, http.MethodPost, d.relaunch, "relaunch job", &created)
	if err != nil {
		var api *APIError
		result.Ambiguous = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			(errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary || api.Kind == Malformed))
		if result.Ambiguous {
			return result, apiError(Connection, "Relaunch outcome unknown; refresh Jobs before retrying")
		}
		if api != nil && api.Kind == Permission {
			result.Source.Capabilities.Relaunch = Capability{Unavailable, "Server denied relaunch; refresh job details"}
		}
		return result, err
	}
	result.Requested = true
	if created.ID <= 0 || created.ID == ref.ID || created.Type != ref.Type ||
		(created.LegacyJob != nil && *created.LegacyJob != created.ID) ||
		(created.LegacyAdHoc != nil && *created.LegacyAdHoc != created.ID) {
		result.Ambiguous = true
		return result, apiError(Malformed, "Relaunch outcome unknown; refresh Jobs before retrying")
	}
	unknown := Capability{Unknown, "Details are not available"}
	result.Job = JobDetails{
		JobSummary:   JobSummary{Ref: JobRef{ID: created.ID, Type: created.Type}, Name: d.Name},
		Capabilities: JobCapabilities{Output: unknown, Cancel: unknown, Children: unknown, Relaunch: unknown},
	}
	return result, nil
}
