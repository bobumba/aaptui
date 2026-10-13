package aap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// SurveyQuestion retains input values without altering their variable names or
// defaults. Only presentation labels are cleaned; callers must clean displayed values.
type SurveyQuestion struct {
	Name, Description, Variable, Type, Default string
	Required                                   bool
	Min, Max                                   *float64
	Choices                                    []string
}

type LaunchRequirements struct {
	Template        TemplateSummary
	AskVariables    bool
	Questions       []SurveyQuestion
	VariablesNeeded []string
}

type LaunchResult struct {
	Job                                 JobDetails
	Requested, Ambiguous, IgnoredFields bool
}

func (c *Client) PrepareLaunch(ctx context.Context, ref TemplateRef) (LaunchRequirements, error) {
	if ref.Type != PlaybookTemplate {
		return LaunchRequirements{}, apiError(Unsupported, "prepare template launch")
	}
	d, err := c.Template(ctx, ref)
	if err != nil {
		return LaunchRequirements{}, err
	}
	if d.Launch.State != Available {
		kind := Unsupported
		if d.Launch.State == Unknown {
			kind = Malformed
		} else if d.startPermission != nil && !*d.startPermission {
			kind = Permission
		}
		return LaunchRequirements{}, apiError(kind, "prepare launch: "+d.Launch.Reason)
	}
	var w struct {
		AskVariables     *bool     `json:"ask_variables_on_launch"`
		SurveyEnabled    *bool     `json:"survey_enabled"`
		Passwords        *[]string `json:"passwords_needed_to_start"`
		InventoryNeeded  *bool     `json:"inventory_needed_to_start"`
		CredentialNeeded *bool     `json:"credential_needed_to_start"`
		VariablesNeeded  *[]string `json:"variables_needed_to_start"`
	}
	if err := c.request(ctx, http.MethodGet, d.launch, "read launch requirements", &w); err != nil {
		return LaunchRequirements{}, err
	}
	if w.AskVariables == nil || w.SurveyEnabled == nil || w.Passwords == nil || w.InventoryNeeded == nil || w.CredentialNeeded == nil || w.VariablesNeeded == nil {
		return LaunchRequirements{}, apiError(Malformed, "launch requirements were not returned")
	}
	if len(*w.Passwords) > 0 || *w.InventoryNeeded || *w.CredentialNeeded {
		return LaunchRequirements{}, apiError(Unsupported, "Launch requires inventory, credentials, or credential passwords; use the controller UI")
	}
	out := LaunchRequirements{Template: d.TemplateSummary, AskVariables: *w.AskVariables, VariablesNeeded: *w.VariablesNeeded}
	if *w.SurveyEnabled {
		link := d.survey
		if link == "" {
			link = c.endpoint(fmt.Sprintf("job_templates/%d/survey_spec/", ref.ID))
		}
		var survey struct {
			Spec *[]struct {
				Name        string          `json:"question_name"`
				Description string          `json:"question_description"`
				Variable    string          `json:"variable"`
				Type        string          `json:"type"`
				Required    *bool           `json:"required"`
				Default     json.RawMessage `json:"default"`
				Min         json.RawMessage `json:"min"`
				Max         json.RawMessage `json:"max"`
				Choices     json.RawMessage `json:"choices"`
			} `json:"spec"`
		}
		if err := c.request(ctx, http.MethodGet, link, "read launch survey", &survey); err != nil {
			return LaunchRequirements{}, err
		}
		if survey.Spec == nil || len(*survey.Spec) == 0 || len(*survey.Spec) > 100 {
			return LaunchRequirements{}, apiError(Malformed, "read launch survey")
		}
		seen := map[string]bool{}
		for i, w := range *survey.Spec {
			op := fmt.Sprintf("Survey question %d", i+1)
			if w.Variable == "" || w.Variable != sanitize(w.Variable) || seen[w.Variable] || w.Required == nil {
				return LaunchRequirements{}, apiError(Malformed, op)
			}
			seen[w.Variable] = true
			switch w.Type {
			case "text", "textarea", "password", "integer", "float", "multiplechoice", "multiselect":
			default:
				return LaunchRequirements{}, apiError(Unsupported, op+" has an unsupported type")
			}
			q := SurveyQuestion{Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Variable: w.Variable, Type: w.Type, Required: *w.Required}
			if q.Name == "" {
				q.Name = op
			}
			q.Min, err = surveyBound(w.Min)
			if err != nil {
				return LaunchRequirements{}, apiError(Malformed, op+" minimum")
			}
			q.Max, err = surveyBound(w.Max)
			if err != nil || (q.Min != nil && q.Max != nil && *q.Min > *q.Max) {
				return LaunchRequirements{}, apiError(Malformed, op+" maximum")
			}
			if w.Type == "text" || w.Type == "textarea" || w.Type == "password" {
				for _, bound := range []*float64{q.Min, q.Max} {
					if bound != nil && (*bound < 0 || math.Trunc(*bound) != *bound) {
						return LaunchRequirements{}, apiError(Malformed, op+" length")
					}
				}
			}
			if len(w.Choices) > 0 && string(w.Choices) != "null" && string(w.Choices) != `""` {
				var list []string
				if json.Unmarshal(w.Choices, &list) != nil {
					var text string
					if json.Unmarshal(w.Choices, &text) != nil {
						return LaunchRequirements{}, apiError(Malformed, op+" choices")
					}
					for _, choice := range strings.Split(text, "\n") {
						if strings.TrimSpace(choice) != "" {
							list = append(list, choice)
						}
					}
				}
				q.Choices = list
			}
			if (q.Type == "multiplechoice" || q.Type == "multiselect") && len(q.Choices) == 0 {
				return LaunchRequirements{}, apiError(Malformed, op+" choices")
			}
			if len(w.Default) > 0 && string(w.Default) != "null" {
				if json.Unmarshal(w.Default, &q.Default) != nil {
					if q.Type == "multiselect" {
						var list []string
						if json.Unmarshal(w.Default, &list) != nil {
							return LaunchRequirements{}, apiError(Malformed, op+" default")
						}
						q.Default = strings.Join(list, "\n")
					} else if q.Type == "integer" || q.Type == "float" {
						var n json.Number
						if json.Unmarshal(w.Default, &n) != nil {
							return LaunchRequirements{}, apiError(Malformed, op+" default")
						}
						q.Default = n.String()
					} else {
						return LaunchRequirements{}, apiError(Malformed, op+" default")
					}
				}
			}
			out.Questions = append(out.Questions, q)
		}
	}
	return out, nil
}

func surveyBound(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("invalid bound")
	}
	return &n, nil
}

// Launch rechecks the current schema and permissions, then issues one POST.
func (c *Client) Launch(ctx context.Context, prepared LaunchRequirements, answers []string, manual string) (LaunchResult, error) {
	current, err := c.PrepareLaunch(ctx, prepared.Template.Ref)
	if err != nil {
		return LaunchResult{}, err
	}
	if !reflect.DeepEqual(current.Questions, prepared.Questions) {
		return LaunchResult{}, apiError(Rejected, "Survey changed; close and reopen the launch form")
	}
	vars, err := LaunchVariables(current, answers, manual)
	if err != nil {
		return LaunchResult{}, err
	}
	var created struct {
		ID       int                        `json:"id"`
		Type     JobType                    `json:"type"`
		Job      *int                       `json:"job"`
		Workflow *int                       `json:"workflow_job"`
		Ignored  map[string]json.RawMessage `json:"ignored_fields"`
	}
	route := c.endpoint(fmt.Sprintf("job_templates/%d/launch/", current.Template.Ref.ID))
	result := LaunchResult{}
	err = c.requestPayload(ctx, http.MethodPost, route, "launch template", map[string]any{"extra_vars": vars}, &created)
	if err != nil {
		var api *APIError
		result.Ambiguous = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary || api.Kind == Malformed))
		if result.Ambiguous {
			return result, apiError(Connection, "Launch outcome unknown; close the form and refresh Jobs before retrying")
		}
		return result, err
	}
	result.Requested = true
	if created.ID <= 0 || (created.Type != PlaybookJob && created.Type != WorkflowJob) ||
		(created.Job != nil && (created.Type != PlaybookJob || *created.Job != created.ID)) ||
		(created.Workflow != nil && (created.Type != WorkflowJob || *created.Workflow != created.ID)) {
		result.Ambiguous = true
		return result, apiError(Malformed, "Launch outcome unknown; close the form and refresh Jobs before retrying")
	}
	result.IgnoredFields = len(created.Ignored) > 0
	unknown := Capability{Unknown, "Details are not available"}
	result.Job = JobDetails{JobSummary: JobSummary{Ref: JobRef{created.ID, created.Type}, Name: current.Template.Name}, Capabilities: JobCapabilities{Output: unknown, Cancel: unknown, Children: unknown, Relaunch: unknown}}
	return result, nil
}

func surveyValue(q SurveyQuestion, answer string) (any, bool, error) {
	if answer == "" {
		if q.Required {
			return nil, false, fmt.Errorf("an answer is required")
		}
		if (q.Type == "text" || q.Type == "textarea") && q.Min != nil && *q.Min == 0 {
			return "", true, nil
		}
		return nil, false, nil
	}
	var value any = answer
	var n float64
	switch q.Type {
	case "text", "textarea", "password":
		if q.Type == "password" && answer == "$encrypted$" {
			if q.Default != "$encrypted$" {
				return nil, false, fmt.Errorf("enter a password")
			}
			return answer, true, nil
		}
		n = float64(len([]rune(answer)))
	case "integer":
		v, err := strconv.ParseInt(answer, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("enter an integer")
		}
		value, n = v, float64(v)
	case "float":
		v, err := strconv.ParseFloat(answer, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false, fmt.Errorf("enter a finite number")
		}
		value, n = v, v
	case "multiplechoice", "multiselect":
		values := []string{answer}
		if q.Type == "multiselect" {
			values = strings.Split(answer, "\n")
		}
		seen := map[string]bool{}
		for _, v := range values {
			found := false
			for _, choice := range q.Choices {
				if v == choice {
					found = true
					break
				}
			}
			if !found || seen[v] {
				return nil, false, fmt.Errorf("select valid, distinct choices")
			}
			seen[v] = true
		}
		if q.Type == "multiselect" {
			value = values
		}
		return value, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported question type")
	}
	if q.Min != nil && n < *q.Min {
		return nil, false, fmt.Errorf("minimum is %g", *q.Min)
	}
	if q.Max != nil && n > *q.Max {
		return nil, false, fmt.Errorf("maximum is %g", *q.Max)
	}
	return value, true, nil
}
