package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeLaunch struct {
	requirements          aap.LaunchRequirements
	prepareErr, launchErr error
	result                aap.LaunchResult
	preparations, posts   int
	answers               []string
	manual                string
}

func (f *fakeLaunch) PrepareLaunch(ctx context.Context, ref aap.TemplateRef) (aap.LaunchRequirements, error) {
	f.preparations++
	if err := ctx.Err(); err != nil {
		return aap.LaunchRequirements{}, err
	}
	return f.requirements, f.prepareErr
}
func (f *fakeLaunch) Launch(ctx context.Context, r aap.LaunchRequirements, answers []string, manual string) (aap.LaunchResult, error) {
	f.posts++
	f.answers = append([]string{}, answers...)
	f.manual = manual
	if err := ctx.Err(); err != nil {
		return aap.LaunchResult{}, err
	}
	return f.result, f.launchErr
}
func (f *fakeLaunch) CleanText(s string) string {
	return strings.ReplaceAll(ansi.Strip(s), "token-secret", "[redacted]")
}

func launchKey(s string) tea.KeyPressMsg {
	k := tea.Key{}
	switch s {
	case "tab":
		k.Code = tea.KeyTab
	case "shift+tab":
		k.Code = tea.KeyTab
		k.Mod = tea.ModShift
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "left":
		k.Code = tea.KeyLeft
	case "right":
		k.Code = tea.KeyRight
	case "home":
		k.Code = tea.KeyHome
	case "end":
		k.Code = tea.KeyEnd
	case "delete":
		k.Code = tea.KeyDelete
	case "space":
		k.Code = tea.KeySpace
		k.Text = " "
	default:
		return key(s)
	}
	return tea.KeyPressMsg(k)
}

func launchModel(t *testing.T, f *fakeLaunch) *Model {
	t.Helper()
	m := New(context.Background(), "gateway").WithJobs(&fakeJobs{}).WithLaunch(f).WithTemplateJobs(&fakeTemplateJobs{})
	cleanupModel(t, m)
	m.screen = templateScreen
	m.templateDetail = aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: aap.TemplateRef{ID: 1, Type: aap.PlaybookTemplate}, Name: "deploy"}, Launch: aap.Capability{State: aap.Available}}
	m.templateJobPage = aap.Page[aap.JobSummary]{Items: []aap.JobSummary{{Ref: aap.JobRef{ID: 4, Type: aap.PlaybookJob}}}, Count: 1}
	m.selected = 0
	m.detailTop = 3
	m.templateJobsFocus = false
	f.requirements.Template = m.templateDetail.TemplateSummary
	f.result = aap.LaunchResult{Requested: true, Job: aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 9, Type: aap.PlaybookJob}, Name: "deploy"}}}
	return m
}
func openLaunch(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(key("L"))
	if cmd == nil || m.launch == nil || !m.loading {
		t.Fatal("launch preparation not started")
	}
	m.Update(cmd())
	if m.launch == nil || !m.launch.ready || m.loading {
		t.Fatal("launch preparation failed", m.err)
	}
}
func sampleQuestions() []aap.SurveyQuestion {
	return []aap.SurveyQuestion{
		{Name: "Title", Variable: "title", Type: "text", Required: true, Default: "部署"},
		{Name: "Password", Variable: "password", Type: "password", Required: true, Default: "$encrypted$"},
		{Name: "Environment", Variable: "environment", Type: "multiplechoice", Required: true, Default: "dev", Choices: []string{"dev", "prod"}},
		{Name: "Targets", Variable: "targets", Type: "multiselect", Choices: []string{"a", "b"}},
		{Name: "Notes", Variable: "notes", Type: "textarea"},
	}
}

func TestLaunchEditorStartsEmptyOnReopen(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: sampleQuestions()}}
	m := launchModel(t, f)
	for attempt := 0; attempt < 2; attempt++ {
		openLaunch(t, m)
		if m.launch.extra.text != "" || m.launch.extra.cursor != 0 {
			t.Fatalf("editor prepopulated on attempt %d: %+v", attempt, m.launch.extra)
		}
		for i, question := range f.requirements.Questions {
			if m.launch.inputs[i].text != question.Default {
				t.Fatal("survey default changed")
			}
		}
		for range m.launch.inputs {
			m.Update(launchKey("tab"))
		}
		m.Update(tea.PasteMsg{Content: "release: v2"})
		m.Update(key("esc"))
	}
}

func TestLaunchEditorDirectInputAndBlankSubmission(t *testing.T) {
	for _, test := range []struct {
		name, input string
		ask         bool
	}{
		{"json", `{"release":"v2","debug":false}`, true},
		{"yaml", "release: v2\ndebug: false", true},
		{"blank", "", false},
		{"whitespace", " \n  ", false},
	} {
		for _, paste := range []bool{false, true} {
			method := "typing"
			if paste {
				method = "paste"
			}
			t.Run(test.name+"/"+method, func(t *testing.T) {
				f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: test.ask}}
				m := launchModel(t, f)
				openLaunch(t, m)
				if paste {
					m.Update(tea.PasteMsg{Content: test.input})
				} else {
					for _, r := range test.input {
						if r == '\n' {
							m.Update(key("enter"))
						} else {
							m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
						}
					}
				}
				if m.launch.extra.text != test.input || m.launch.extra.cursor != len([]rune(test.input)) || f.posts != 0 {
					t.Fatal("input changed or submitted prematurely")
				}
				if !strings.Contains(ansi.Strip(m.View().Content), "JSON or YAML") {
					t.Fatal("format hint missing")
				}
				m.Update(launchKey("tab"))
				_, cmd := m.Update(key("enter"))
				if cmd == nil {
					t.Fatal("launch failed validation", m.err)
				}
				m.Update(cmd())
				if f.posts != 1 || f.manual != test.input || m.launch != nil || m.screen != jobScreen {
					t.Fatal("manual input not submitted unchanged")
				}
				vars, err := aap.ParseExtraVars(f.manual)
				if err != nil || test.ask && (vars["release"] != "v2" || vars["debug"] != false) || !test.ask && len(vars) != 0 {
					t.Fatalf("wrong parsed variables: %v, %v", vars, err)
				}
			})
		}
	}
}

func TestLaunchFormFocusInputsAndNavigation(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: sampleQuestions()}}
	m := launchModel(t, f)
	if !strings.Contains(ansi.Strip(m.View().Content), "Launch (L)") || !strings.Contains(ansi.Strip(strings.Join(m.screenHelp(80), "\n")), "L launch job") {
		t.Fatal("missing launch action")
	}
	openLaunch(t, m)
	// Real Tab and Shift-Tab messages traverse each field/editor/button and wrap.
	count := len(f.requirements.Questions) + 3
	for i := 1; i <= count; i++ {
		m.Update(launchKey("tab"))
		if m.launch.focus != i%count {
			t.Fatal("Tab focus")
		}
	}
	m.Update(launchKey("shift+tab"))
	if m.launch.focus != count-1 {
		t.Fatal("reverse focus")
	}
	m.Update(launchKey("tab"))
	// Global shortcuts are ordinary text inside survey inputs.
	for _, s := range []string{"q", "L", "r", "c"} {
		m.Update(key(s))
	}
	if m.quitting || m.launch.inputs[0].text != "部署qLrc" || m.screen != templateScreen {
		t.Fatal("global shortcut consumed input")
	}
	m.Update(key("ctrl+u"))
	m.Update(tea.PasteMsg{Content: "部署"})
	m.Update(launchKey("tab"))
	m.Update(key("s"))
	m.Update(tea.PasteMsg{Content: "uper-secret"})
	if m.launch.inputs[1].text != "super-secret" {
		t.Fatal("saved default was not replaced")
	}
	if strings.Contains(m.View().Content, "super-secret") {
		t.Fatal("password exposed")
	}
	m.Update(launchKey("tab"))
	m.Update(launchKey("down"))
	m.Update(launchKey("space"))
	if m.launch.inputs[2].text != "prod" {
		t.Fatal("choice selection")
	}
	m.Update(launchKey("tab"))
	m.Update(launchKey("space"))
	m.Update(launchKey("down"))
	m.Update(launchKey("space"))
	if m.launch.inputs[3].text != "a\nb" {
		t.Fatal("multiselect")
	}
	m.Update(launchKey("tab"))
	m.Update(key("first"))
	m.Update(key("enter"))
	m.Update(key("second"))
	if m.launch.inputs[4].text != "first\nsecond" || f.posts != 0 {
		t.Fatal("textarea enter submitted")
	}
	m.Update(launchKey("tab"))
	m.Update(tea.PasteMsg{Content: "manual: true"})
	m.Update(key("enter"))
	m.Update(key("extra: 2"))
	if m.launch.extra.text != "manual: true\nextra: 2" || f.posts != 0 {
		t.Fatal("extra_vars editing")
	}
	m.Update(launchKey("tab"))
	_, cmd := m.Update(key("enter"))
	if cmd == nil || !strings.Contains(m.View().Content, "Launching job") {
		t.Fatal("missing submit progress", m.err)
	}
	for _, s := range []string{"enter", "space", "tab", "L", "r"} {
		if _, duplicate := m.Update(launchKey(s)); duplicate != nil {
			t.Fatal("duplicate submission")
		}
	}
	_, load := m.Update(cmd())
	if f.posts != 1 || m.launch != nil || m.screen != jobScreen || m.jobDetail.Ref.ID != 9 || load == nil || len(m.stack) != 1 {
		t.Fatal("success did not open job")
	}
	m.Update(load())
	m.Update(key("esc"))
	if m.screen != templateScreen || m.detailTop != 3 || m.templateJobsFocus || m.templateJobPage.Count != 1 || len(m.stack) != 0 {
		t.Fatal("template context lost")
	}
}

func TestLaunchWithoutSurveyDismissalAndValidation(t *testing.T) {
	for _, dismiss := range []string{"esc", "button", "ctrl+c"} {
		f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}}
		m := launchModel(t, f)
		openLaunch(t, m)
		if len(m.launch.inputs) != 0 || m.launch.focus != 0 {
			t.Fatal("no-survey focus")
		}
		m.Update(launchKey("shift+tab"))
		if dismiss == "button" {
			m.Update(key("enter"))
		} else {
			m.Update(key(dismiss))
		}
		if m.launch != nil || f.posts != 0 || m.screen != templateScreen {
			t.Fatal("dismissal mutated or navigated")
		}
	}
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: []aap.SurveyQuestion{{Name: "Required", Variable: "required", Type: "text", Required: true}}}}
	m := launchModel(t, f)
	openLaunch(t, m)
	m.launch.focus = 2
	if _, cmd := m.Update(key("enter")); cmd != nil || f.posts != 0 || m.err == nil {
		t.Fatal("required field accepted")
	}
	m.launch.inputs[0].text = "deploy"
	m.launch.extra.text = "required: secret"
	if _, cmd := m.Update(key("enter")); cmd != nil || m.err == nil || !strings.Contains(m.err.Error(), "survey field") || strings.Contains(m.err.Error(), "secret") {
		t.Fatal("collision or value exposed", m.err)
	}
	m.launch.extra.text = "broken: [secret"
	if _, cmd := m.Update(key("enter")); cmd != nil || m.err == nil || strings.Contains(m.err.Error(), "secret") {
		t.Fatal("invalid YAML accepted", m.err)
	}
	m.launch.extra.text = "manual: true"
	m.launch.requirements.AskVariables = false
	if _, cmd := m.Update(key("enter")); cmd != nil || m.err == nil || !strings.Contains(m.err.Error(), "Prompt on launch") {
		t.Fatal("restricted manual variables accepted")
	}
}

func TestLaunchFailurePreservesInputsAndUncertainOutcomeBlocksRetry(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}, launchErr: errors.New("safe failure")}
		m := launchModel(t, f)
		openLaunch(t, m)
		f.result = aap.LaunchResult{Ambiguous: uncertain}
		m.launch.extra.text = "manual: true"
		m.launch.focus = 1
		_, cmd := m.Update(key("enter"))
		m.Update(cmd())
		if m.err == nil || m.launch == nil || m.launch.extra.text != "manual: true" || m.launch.uncertain != uncertain || m.loading {
			t.Fatal("failure inputs lost")
		}
		_, retry := m.Update(key("enter"))
		if (retry == nil) != uncertain {
			t.Fatal("retry policy incorrect")
		}
		m.Update(key("esc"))
	}
}

func TestLaunchPreparationRestrictionsAndStaleResults(t *testing.T) {
	for _, state := range []aap.CapabilityState{aap.Unavailable, aap.Unknown, ""} {
		f := &fakeLaunch{}
		m := launchModel(t, f)
		m.templateDetail.Launch = aap.Capability{State: state, Reason: "Not permitted"}
		if _, cmd := m.Update(key("L")); cmd != nil || m.launch != nil || m.err == nil {
			t.Fatal("unavailable launch opened")
		}
	}
	for _, screen := range []screen{mainScreen, jobsScreen, jobScreen, projectsScreen} {
		f := &fakeLaunch{}
		m := launchModel(t, f)
		m.screen = screen
		if _, cmd := m.Update(key("L")); cmd != nil || m.launch != nil {
			t.Fatal("launch outside template")
		}
	}
	f := &fakeLaunch{prepareErr: errors.New("safe permission failure")}
	m := launchModel(t, f)
	_, cmd := m.Update(key("L"))
	m.Update(cmd())
	if m.launch == nil || m.launch.ready || m.err == nil || m.loading {
		t.Fatal("preparation error lost")
	}
	m.Update(key("esc"))
	for _, stage := range []string{"prepare", "submit"} {
		f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}}
		m := launchModel(t, f)
		_, cmd := m.Update(key("L"))
		if stage == "submit" {
			m.Update(cmd())
			m.launch.focus = 1
			_, cmd = m.Update(key("enter"))
		}
		result := cmd()
		m.Update(key("esc"))
		m.Update(result)
		if m.launch != nil || m.screen != templateScreen || m.loading || len(m.stack) != 0 {
			t.Fatal("stale launch result applied")
		}
		if stage == "submit" && !strings.Contains(m.notice, "may still have created a job") {
			t.Fatal("uncertain pending dismissal unexplained")
		}
	}
}

func TestLaunchIdentityRetainedAndIgnoredFieldsVisible(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}}
	m := launchModel(t, f)
	m.WithJobs(&inaccessibleJobs{})
	openLaunch(t, m)
	f.result.IgnoredFields = true
	m.launch.focus = 1
	_, cmd := m.Update(key("enter"))
	_, load := m.Update(cmd())
	m.Update(load())
	if m.screen != jobScreen || m.jobDetail.Ref.ID != 9 || m.err == nil || !strings.Contains(ansi.Strip(m.View().Content), "controller ignored launch inputs") {
		t.Fatal("identity/warning lost on detail failure")
	}
}

func TestLaunchFramesFocusAndSecretRendering(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: sampleQuestions()}}
	m := launchModel(t, f)
	openLaunch(t, m)
	m.launch.inputs[1].text = "super-secret"
	m.launch.inputs[1].cursor = 12
	m.launch.extra.text = "password: token-secret\nmessage: \x1b[31mred"
	for _, size := range [][2]int{{1, 1}, {2, 2}, {19, 6}, {30, 10}, {40, 12}, {80, 24}} {
		m.width, m.height = size[0], size[1]
		for i := 0; i < len(m.launch.inputs)+3; i++ {
			m.launch.focus = i
			assertFrameSize(t, m)
			view := ansi.Strip(m.View().Content)
			if strings.Contains(view, "super-secret") || strings.Contains(view, "token-secret") {
				t.Fatal("secret exposed")
			}
			if size[0] >= 30 && size[1] >= 10 && i == len(m.launch.inputs)+1 && !strings.Contains(view, "Launch job") {
				t.Fatal("focused submit button not visible", view)
			}
		}
	}
}

func TestLaunchEditorRedactsTokenAcrossCursor(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}}
	m := launchModel(t, f)
	openLaunch(t, m)
	m.launch.extra = launchInput{text: "token-secret", cursor: 5}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "[redacted]") || strings.Contains(view, "token") || strings.Contains(view, "secret") {
		t.Fatal("cursor bypassed token redaction")
	}
}

func TestLaunchRuneEditorCursorEditingAndBounds(t *testing.T) {
	in := launchInput{text: "部署\nsecond", cursor: 2}
	edit := func(k string) {
		t.Helper()
		if err := in.update(launchKey(k), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.update(launchKey("left"), true); err != nil {
		t.Fatal(err)
	}
	if err := in.insert("新", true); err != nil {
		t.Fatal(err)
	}
	if in.text != "部新署\nsecond" || in.cursor != 2 {
		t.Fatal("rune insertion")
	}
	edit("end")
	edit("down")
	if in.cursor != 7 {
		t.Fatal("multiline down")
	}
	edit("home")
	edit("delete")
	if in.text != "部新署\necond" {
		t.Fatal("delete")
	}
	edit("up")
	if in.cursor != 0 {
		t.Fatal("multiline up")
	}
	edit("backspace")
	if in.text != "部新署\necond" {
		t.Fatal("backspace at start")
	}
	if err := in.insert(strings.Repeat("x", aap.LaunchInputBytes), true); err == nil {
		t.Fatal("oversized input accepted")
	}
	in = launchInput{}
	if err := in.insert("a\r\nb\tc\x1b", false); err != nil || in.text != "a b c" {
		t.Fatal("single line paste controls", err)
	}
}
