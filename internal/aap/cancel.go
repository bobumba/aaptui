package aap

import (
	"context"
	"errors"
	"net/http"
)

type CancellationResult struct {
	Job                  JobDetails
	Requested, Ambiguous bool
}

// Cancel performs exactly one mutation after a fresh type/state/permission check.
// An ambiguous transport response is reconciled by reading, never by retrying POST.
func (c *Client) Cancel(ctx context.Context, ref JobRef) (CancellationResult, error) {
	d, err := c.Job(ctx, ref)
	if err != nil {
		return CancellationResult{}, err
	}
	result := CancellationResult{Job: d}
	if d.Capabilities.Cancel.State != Available {
		kind := Unsupported
		if d.cancel != "" && d.Status.Active() {
			kind = Permission
		}
		return result, apiError(kind, "cancel job")
	}
	err = c.request(ctx, http.MethodPost, d.cancel, "cancel job", nil)
	result.Requested = err == nil
	if err != nil {
		var api *APIError
		result.Ambiguous = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary))
		if !result.Ambiguous {
			if api != nil && api.Kind == Unsupported {
				updated, refreshErr := c.Job(ctx, ref)
				if refreshErr != nil {
					return result, errors.Join(err, refreshErr)
				}
				result.Job = updated
			}
			if api != nil && api.Kind == Permission {
				result.Job.Capabilities.Cancel = Capability{Unavailable, err.Error()}
			}
			return result, err
		}
	}
	updated, refreshErr := c.Job(ctx, ref)
	if refreshErr == nil {
		result.Job = updated
	}
	if result.Ambiguous {
		if refreshErr != nil {
			return result, apiError(Connection, "Cancellation outcome unknown; refresh job status")
		}
		return result, apiError(Connection, "Cancellation outcome unknown; job status refreshed")
	}
	if refreshErr != nil {
		return result, apiError(Connection, "Cancellation requested; status refresh failed")
	}
	return result, nil
}
