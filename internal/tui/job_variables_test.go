package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Strip only the box edges so assertions can read wrapped content independently
// of decoration. Border placement is checked separately below.
func normalizedVariableText(text string) string {
	var lines []string
	for _, line := range strings.Split(ansi.Strip(text), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "╭") || strings.HasPrefix(line, "╰") {
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│")
		lines = append(lines, line)
	}
	return normalizedDetailText(strings.Join(lines, "\n"))
}

func TestVariableBoxStates(t *testing.T) {
	for _, template := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, test := range []struct {
				name string
				vars *aap.JobVariables
				want string
			}{
				{"unsupported", nil, "Variables: Not supported for this job type"},
				{"empty", &aap.JobVariables{}, "Variables: No recorded variables"},
				{"missing", &aap.JobVariables{UnavailableReason: "Not returned by the server"}, "Variables: Unavailable · Not returned by the server"},
				{"malformed", &aap.JobVariables{UnavailableReason: "Malformed variable data"}, "Variables: Unavailable · Malformed variable data"},
				{"oversized", &aap.JobVariables{UnavailableReason: "Variable data exceeds 64 KiB"}, "Variables: Unavailable · Variable data exceeds 64 KiB"},
				{"populated", &aap.JobVariables{Values: []aap.JobVariable{{Name: "environment", Value: `"prod"`}, {Name: "enabled", Value: "false"}, {Name: "count", Value: "0"}, {Name: "password", Value: `"$encrypted$"`}}}, `environment: "prod" enabled: false count: 0 password: "$encrypted$"`},
			} {
				t.Run(fmt.Sprintf("template=%v/dark=%v/%s", template, dark, test.name), func(t *testing.T) {
					m := New(context.Background(), "")
					cleanupModel(t, m)
					m.screen = jobScreen
					m.styles = newStyles(dark)
					m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "retained job"}, ExtraVars: test.vars}
					title, name, want := "Survey / Extra variables", "retained job", test.want
					if template {
						m.screen = templateScreen
						m.templateDetail = aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: aap.TemplateRef{ID: 1, Type: aap.PlaybookTemplate}, Name: "retained template"}, ExtraVars: test.vars}
						title, name = "Extra variables", "retained template"
						if test.vars == nil {
							want = "Variables: Unavailable · Not returned by the server"
						} else if len(test.vars.Values) == 0 && test.vars.UnavailableReason == "" {
							want = "Variables: No configured variables"
						}
					}
					text := ansi.Strip(strings.Join(m.details(100), "\n"))
					details, variables, ok := strings.Cut(normalizedVariableText(text), title)
					if !ok || !strings.Contains(details, "Name: "+name) || !strings.Contains(variables, want) {
						t.Fatalf("missing details or variable state: %s", text)
					}
					if strings.Count(text, "╭") != 1 || strings.Count(text, "╰") != 1 {
						t.Fatalf("expected one variables box: %s", text)
					}
					_, box, _ := strings.Cut(text, "╭")
					box, _, _ = strings.Cut(box, "╰")
					if !strings.Contains(normalizedVariableText(box), want) {
						t.Fatalf("variable content outside the box: %s", text)
					}
				})
			}
		}
	}
}

// Synthetic HTTPS fixtures drive real API decoding, navigation and refresh.
func TestJobVariablesIntegrationBothModes(t *testing.T) {
	for _, template := range []bool{false, true} {
		for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
			t.Run(fmt.Sprintf("template=%v/%s", template, mode), func(t *testing.T) {
				prefix := "/api/controller/v2/"
				if mode == config.Direct {
					prefix = "/api/v2/"
				}
				route, kind, name, title := "jobs/1/", "job", "recorded run", "Survey / Extra variables"
				value := "recorded-prod"
				if template {
					route, kind, name, title = "job_templates/1/", "job_template", "configured template", "Extra variables"
					value = "configured-prod"
				}
				calls := 0
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != prefix+route || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
						t.Errorf("unexpected job request: %s %s", r.Method, r.URL)
						http.NotFound(w, r)
						return
					}
					calls++
					vars := `{"environment":"` + value + `","password":"$encrypted$","notes":"first\nlast","nested":{"items":[false,0,"secret\u001b[31m"]}}`
					if calls > 1 {
						vars = "invalid-private-value"
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": kind, "name": name, "status": "successful", "extra_vars": vars}); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
				if err != nil {
					t.Fatal(err)
				}
				m := New(context.Background(), "").WithJobs(client).WithTemplates(client)
				cleanupModel(t, m)
				m.screen, m.width, m.height = jobsScreen, 110, 70
				m.jobPage.Items = []aap.JobSummary{{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "recorded run"}}
				if template {
					m.screen = templatesScreen
					m.height = 100
					m.WithTemplateJobs(&fakeTemplateJobs{page: aap.Page[aap.JobSummary]{Count: 2, Items: []aap.JobSummary{{Name: "first"}, {Name: "selected"}}}})
					m.templatePage.Items = []aap.TemplateSummary{{Ref: aap.TemplateRef{ID: 1, Type: aap.PlaybookTemplate}, Name: name}}
				}
				listScreen := m.screen
				_, cmd := m.Update(key("enter"))
				if cmd == nil {
					t.Fatal("job details request missing")
				}
				_, jobsCmd := m.Update(cmd())
				if template {
					if jobsCmd == nil {
						t.Fatal("template job table not loaded")
					}
					m.Update(jobsCmd())
					m.Update(key("tab"))
					m.selected = 1
				}
				text := normalizedVariableText(m.View().Content)
				for _, want := range []string{title, `environment: "` + value + `"`, `password: "$encrypted$"`, "notes: first last", "false", "0", "[redacted]"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q in %s", want, text)
					}
				}
				if strings.Contains(text, "secret") || strings.Contains(text, "\\u001b") || calls != 1 {
					t.Fatal("unsafe text or unnecessary template/survey request")
				}
				assertFrameSize(t, m)
				_, cmd = m.Update(key("r"))
				if cmd == nil {
					t.Fatal("refresh missing")
				}
				_, followup := m.Update(cmd())
				if template && (followup != nil || m.templateJobsFocus || m.selected != 1 || m.templateJobPage.Count != 2) {
					t.Fatal("detail refresh changed job table or focus")
				}
				text = normalizedVariableText(m.View().Content)
				if !strings.Contains(text, "Name: "+name) || !strings.Contains(text, "Unavailable · Malformed variable data") || strings.Contains(text, "invalid-private-value") || strings.Contains(text, value) {
					t.Fatalf("refresh retained old variables or lost job: %s", text)
				}
				m.Update(key("esc"))
				if m.screen != listScreen || m.selected != 0 {
					t.Fatal("job list context lost")
				}
			})
		}
	}
}

func TestJobVariableWrappingScrollingAndResize(t *testing.T) {
	for _, template := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			m.styles = newStyles(dark)
			m.screen, m.width, m.height = jobScreen, 80, 24
			m.jobDetail = aap.JobDetails{
				JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "variable run"},
				ExtraVars: &aap.JobVariables{Values: []aap.JobVariable{
					{Name: "survey_notes", Value: "first\n\n" + strings.Repeat("部署 café ", 20) + "last"},
					{Name: "nested", Value: "{\n  \"items\": [false, 0, null]\n}"},
					{Name: "last_variable", Value: "VARIABLE_TAIL"},
				}},
			}
			if template {
				m.screen = templateScreen
				m.WithTemplateJobs(&fakeTemplateJobs{})
				m.templateDetail = aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: aap.TemplateRef{ID: 1, Type: aap.PlaybookTemplate}}, ExtraVars: m.jobDetail.ExtraVars}
			}
			expectedScreen := m.screen
			for _, width := range []int{160, 80, 30, 8} {
				for _, line := range m.details(width) {
					if ansi.StringWidth(line) > width {
						t.Fatalf("detail exceeds width %d: %q", width, line)
					}
				}
			}
			for _, dimensions := range [][2]int{{160, 24}, {30, 12}, {80, 24}, {8, 6}, {80, 24}} {
				m.Update(tea.WindowSizeMsg{Width: dimensions[0], Height: dimensions[1]})
				assertFrameSize(t, m)
				if m.screen != expectedScreen || m.selected != 0 {
					t.Fatal("resize changed navigation")
				}
			}
			sawTail := false
			for i := 0; i < len(m.details(80)); i++ {
				m.Update(key("j"))
				assertFrameSize(t, m)
				if strings.Contains(ansi.Strip(m.View().Content), "VARIABLE_TAIL") {
					sawTail = true
					break
				}
			}
			if !sawTail {
				t.Fatalf("last variable inaccessible (template=%v, dark=%v)", template, dark)
			}
		}
	}
}
