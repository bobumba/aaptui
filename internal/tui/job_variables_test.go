package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

func TestJobVariableSectionStates(t *testing.T) {
	for _, test := range []struct {
		name string
		vars *aap.JobVariables
		want string
	}{
		{"unsupported", nil, "Variables: Not supported for this job type"},
		{"empty", &aap.JobVariables{}, "Variables: No recorded variables"},
		{"missing", &aap.JobVariables{UnavailableReason: "Not returned by the server"}, "Variables: Unavailable · Not returned by the server"},
		{"malformed", &aap.JobVariables{UnavailableReason: "Malformed variable data"}, "Variables: Unavailable · Malformed variable data"},
		{"populated", &aap.JobVariables{Values: []aap.JobVariable{{Name: "environment", Value: `"prod"`}, {Name: "enabled", Value: "false"}, {Name: "count", Value: "0"}, {Name: "password", Value: `"$encrypted$"`}}}, `environment: "prod" enabled: false count: 0 password: "$encrypted$"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			m.screen = jobScreen
			m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "retained job"}, ExtraVars: test.vars}
			text := ansi.Strip(strings.Join(m.details(100), "\n"))
			details, variables, ok := strings.Cut(text, "Survey / Extra variables\n")
			if !ok || !strings.Contains(normalizedDetailText(details), "Name: retained job") || !strings.Contains(normalizedDetailText(variables), test.want) {
				t.Fatalf("missing details or variable state: %s", text)
			}
		})
	}
}

// Synthetic HTTPS fixtures drive real API decoding, job navigation and refresh.
func TestJobVariablesIntegrationBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != prefix+"jobs/1/" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("unexpected job request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				calls++
				vars := `{"environment":"recorded-prod","password":"$encrypted$","notes":"first\nlast","nested":{"items":[false,0,"secret\u001b[31m"]}}`
				if calls > 1 {
					vars = "invalid-private-value"
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "name": "recorded run", "status": "successful", "extra_vars": vars}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			m := New(context.Background(), "").WithJobs(client)
			cleanupModel(t, m)
			m.screen, m.width, m.height = jobsScreen, 110, 70
			m.jobPage.Items = []aap.JobSummary{{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "recorded run"}}
			_, cmd := m.Update(key("enter"))
			if cmd == nil {
				t.Fatal("job details request missing")
			}
			m.Update(cmd())
			text := normalizedDetailText(m.View().Content)
			for _, want := range []string{"Survey / Extra variables", `environment: "recorded-prod"`, `password: "$encrypted$"`, "notes: first last", "false", "0", "[redacted]"} {
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
			m.Update(cmd())
			text = normalizedDetailText(m.View().Content)
			if !strings.Contains(text, "Name: recorded run") || !strings.Contains(text, "Unavailable · Malformed variable data") || strings.Contains(text, "invalid-private-value") || strings.Contains(text, "recorded-prod") {
				t.Fatalf("refresh retained old variables or lost job: %s", text)
			}
			m.Update(key("esc"))
			if m.screen != jobsScreen || m.selected != 0 {
				t.Fatal("job list context lost")
			}
		})
	}
}

func TestJobVariableWrappingScrollingAndResize(t *testing.T) {
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
			if m.screen != jobScreen || m.selected != 0 {
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
			t.Fatalf("last variable inaccessible (dark=%v)", dark)
		}
	}
}
