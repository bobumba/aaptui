// Package tui owns interactive state. Only Update mutates the model.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type screen int

const (
	mainScreen screen = iota
	templatesScreen
	jobsScreen
	templateScreen
	jobScreen
	workflowScreen
	outputScreen
	projectsScreen
	inventoriesScreen
	inventoryScreen
	groupsScreen
	groupScreen
	hostsScreen
	hostScreen
	nodeScreen
)

type Canceller interface {
	Cancel(context.Context, aap.JobRef) (aap.CancellationResult, error)
}
type OutputSession interface {
	Read(context.Context, int, int) (aap.OutputUpdate, error)
	Next(int) (aap.OutputUpdate, error)
	Stop()
	Close() error
}
type WorkflowReader interface {
	WorkflowNodes(context.Context, aap.JobRef, aap.ListOptions) (aap.Page[aap.WorkflowNode], error)
}
type JobReader interface {
	ListJobs(context.Context, aap.ListOptions) (aap.Page[aap.JobSummary], error)
	Job(context.Context, aap.JobRef) (aap.JobDetails, error)
}
type TemplateReader interface {
	ListTemplates(context.Context, aap.ListOptions) (aap.Page[aap.TemplateSummary], error)
	Template(context.Context, aap.TemplateRef) (aap.TemplateDetails, error)
}
type ProjectReader interface {
	ListProjects(context.Context, aap.ListOptions) (aap.Page[aap.TemplateSummary], error)
	Template(context.Context, aap.TemplateRef) (aap.TemplateDetails, error)
}
type frame struct {
	inventoryDetail    aap.InventoryDetails
	groupDetail        aap.GroupDetails
	hostDetail         aap.HostDetails
	nodeDetail         aap.WorkflowNode
	objectDetailsFocus bool
	templateJobs       aap.Page[aap.JobSummary]
	templateJobsFocus  bool
	inventory          aap.InventorySummary
	group              aap.GroupSummary
	groups             aap.Page[aap.GroupSummary]
	hosts              aap.Page[aap.HostSummary]
	inventories        aap.Page[aap.InventorySummary]
	detailTop          int
	job                aap.JobDetails
	nodes              aap.Page[aap.WorkflowNode]
	jobs               aap.Page[aap.JobSummary]
	templates          aap.Page[aap.TemplateSummary]
	template           aap.TemplateDetails
	screen             screen
	selected           int
	search             string
}
type resultMsg struct {
	id    uint64
	value any
	err   error
}
type Model struct {
	hostGroups                   HostGroupsReader
	hostGroupPage                aap.Page[aap.GroupSummary]
	hostGroupsKnown              bool
	hostGroupsLoading            bool
	hostGroupsErr                error
	hostGroupsID                 uint64
	hostGroupsCancel             context.CancelFunc
	objectDetails                ObjectDetailsReader
	inventoryDetail              aap.InventoryDetails
	groupDetail                  aap.GroupDetails
	hostDetail                   aap.HostDetails
	nodeDetail                   aap.WorkflowNode
	objectDetailsFocus           bool
	runningJobs                  RunningJobsPoller
	runningPending, runningKnown bool
	runningCount                 int
	runningErr                   error
	templateJobs                 TemplateJobsReader
	templateJobPage              aap.Page[aap.JobSummary]
	templateJobsFocus            bool
	serverURL                    string
	userReader                   UserReader
	user                         aap.User
	userLoading                  bool
	userErr                      error
	inventoryContents            InventoryContentsReader
	inventory                    aap.InventorySummary
	group                        aap.GroupSummary
	groupPage                    aap.Page[aap.GroupSummary]
	hostPage                     aap.Page[aap.HostSummary]
	inventories                  InventoryReader
	inventoryPage                aap.Page[aap.InventorySummary]
	styles                       styles
	detailTop, outputLeft        int
	canceller                    Canceller
	confirming                   bool
	confirmation                 aap.JobRef
	outputID                     uint64
	outputPending, following     bool
	openOutput                   func(context.Context, aap.JobRef) OutputSession
	output                       OutputSession
	sessions                     []OutputSession
	outputUpdate                 aap.OutputUpdate
	outputTop                    int
	cleanupErr                   error
	workflows                    WorkflowReader
	nodePage                     aap.Page[aap.WorkflowNode]
	jobs                         JobReader
	jobPage                      aap.Page[aap.JobSummary]
	jobDetail                    aap.JobDetails
	templates                    TemplateReader
	projects                     ProjectReader
	templatePage                 aap.Page[aap.TemplateSummary]
	templateDetail               aap.TemplateDetails
	ctx                          context.Context
	cancel                       context.CancelFunc
	requestCancel                context.CancelFunc
	generation                   uint64
	screen                       screen
	stack                        []frame
	selected, width, height      int
	search                       string
	editing, loading, quitting   bool
	err                          error
	connection                   string
}

func New(ctx context.Context, connection string) *Model {
	ctx, cancel := context.WithCancel(ctx)
	return &Model{ctx: ctx, cancel: cancel, connection: connection, width: 80, height: 24, styles: newStyles(true)}
}
func (m *Model) WithTemplates(reader TemplateReader) *Model { m.templates = reader; return m }
func (m *Model) WithProjects(reader ProjectReader) *Model   { m.projects = reader; return m }
func (m *Model) loadProjects(cursor aap.PageCursor) tea.Cmd {
	if m.projects == nil {
		return nil
	}
	search := m.search
	return m.begin(func(ctx context.Context) (any, error) {
		return m.projects.ListProjects(ctx, aap.ListOptions{Search: search, Cursor: cursor})
	})
}
func (m *Model) loadTemplates(cursor aap.PageCursor) tea.Cmd {
	if m.templates == nil {
		return nil
	}
	search := m.search
	return m.begin(func(ctx context.Context) (any, error) {
		return m.templates.ListTemplates(ctx, aap.ListOptions{Search: search, Cursor: cursor})
	})
}
func (m *Model) WithJobs(reader JobReader) *Model { m.jobs = reader; return m }
func (m *Model) loadJobs(cursor aap.PageCursor) tea.Cmd {
	if m.jobs == nil {
		return nil
	}
	search := m.search
	return m.begin(func(ctx context.Context) (any, error) {
		return m.jobs.ListJobs(ctx, aap.ListOptions{Search: search, Cursor: cursor})
	})
}
func (m *Model) loadJob(ref aap.JobRef) tea.Cmd {
	if m.jobs == nil {
		return nil
	}
	if m.jobDetail.Ref != ref {
		summary := aap.JobSummary{Ref: ref}
		for _, job := range m.jobPage.Items {
			if job.Ref == ref {
				summary = job
				break
			}
		}
		for _, job := range m.templateJobPage.Items {
			if job.Ref == ref {
				summary = job
				break
			}
		}
		for _, node := range m.nodePage.Items {
			if node.Job != nil && *node.Job == ref {
				summary.Name = node.Name
				summary.Status = node.Status
				break
			}
		}
		unknown := aap.Capability{State: aap.Unknown, Reason: "Details are not available"}
		m.jobDetail = aap.JobDetails{JobSummary: summary, Capabilities: aap.JobCapabilities{Output: unknown, Cancel: unknown, Children: unknown}}
	}
	return m.begin(func(ctx context.Context) (any, error) { return m.jobs.Job(ctx, ref) })
}
func (m *Model) WithWorkflows(reader WorkflowReader) *Model { m.workflows = reader; return m }
func (m *Model) loadNodes(cursor aap.PageCursor) tea.Cmd {
	if m.workflows == nil {
		return nil
	}
	ref := m.jobDetail.Ref
	return m.begin(func(ctx context.Context) (any, error) {
		return m.workflows.WorkflowNodes(ctx, ref, aap.ListOptions{Cursor: cursor})
	})
}
func (m *Model) WithOutput(open func(context.Context, aap.JobRef) OutputSession) *Model {
	m.openOutput = open
	return m
}
func (m *Model) readOutput(start int) tea.Cmd {
	if m.output == nil {
		return nil
	}
	session := m.output
	end := start + m.outputLines()
	return m.begin(func(ctx context.Context) (any, error) { return session.Read(ctx, start, end) })
}
func (m *Model) closeOutput() tea.Cmd {
	if m.output == nil {
		return nil
	}
	s := m.output
	m.outputID++
	m.outputPending = false
	s.Stop()
	m.output = nil
	return func() tea.Msg { return cleanupMsg{session: s, err: s.Close()} }
}

type liveMsg struct {
	id     uint64
	update aap.OutputUpdate
	err    error
}

func (m *Model) nextOutput() tea.Cmd {
	if m.output == nil || m.outputPending {
		return nil
	}
	session := m.output
	id := m.outputID
	lines := m.outputLines()
	m.outputPending = true
	return func() tea.Msg { update, err := session.Next(lines); return liveMsg{id, update, err} }
}

type cleanupMsg struct {
	session OutputSession
	err     error
}

func (m *Model) WithCancellation(c Canceller) *Model { m.canceller = c; return m }
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.loadUser(), m.nextRunningJobs())
}
func (m *Model) Close() error {
	m.cancel()
	if m.requestCancel != nil {
		m.requestCancel()
	}
	var errs []error
	for _, s := range m.sessions {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.cleanupErr != nil {
		errs = append(errs, m.cleanupErr)
	}
	return errors.Join(errs...)
}
func (m *Model) begin(work func(context.Context) (any, error)) tea.Cmd {
	if m.requestCancel != nil {
		m.requestCancel()
	}
	m.generation++
	id := m.generation
	ctx, cancel := context.WithCancel(m.ctx)
	m.requestCancel = cancel
	m.loading = true
	m.err = nil
	return func() tea.Msg { v, err := work(ctx); return resultMsg{id, v, err} }
}
func (m *Model) move(s screen) {
	m.stack = append(m.stack, frame{
		templateJobs: m.templateJobPage, templateJobsFocus: m.templateJobsFocus,
		screen: m.screen, selected: m.selected, search: m.search, detailTop: m.detailTop,
		job: m.jobDetail, nodes: m.nodePage, jobs: m.jobPage,
		templates: m.templatePage, template: m.templateDetail,
		inventories: m.inventoryPage, inventory: m.inventory,
		group: m.group, groups: m.groupPage, hosts: m.hostPage,
		inventoryDetail: m.inventoryDetail, groupDetail: m.groupDetail, hostDetail: m.hostDetail,
		nodeDetail: m.nodeDetail, objectDetailsFocus: m.objectDetailsFocus,
	})
	m.invalidate()
	m.screen = s
	m.selected = 0
	m.search = ""
	m.detailTop = 0
	m.objectDetailsFocus = false
}
func (m *Model) invalidate() {
	m.invalidateHostGroups()
	if m.requestCancel != nil {
		m.requestCancel()
	}
	m.generation++
	m.loading = false
	m.err = nil
	m.editing = false
	m.confirming = false
}
func (m *Model) back() {
	m.invalidate()
	if n := len(m.stack); n > 0 {
		f := m.stack[n-1]
		m.stack = m.stack[:n-1]
		m.screen = f.screen
		m.selected = f.selected
		m.detailTop = f.detailTop
		m.search = f.search
		m.jobDetail = f.job
		m.nodePage = f.nodes
		m.jobPage = f.jobs
		m.templatePage = f.templates
		m.templateDetail = f.template
		m.templateJobPage = f.templateJobs
		m.templateJobsFocus = f.templateJobsFocus
		m.inventoryPage = f.inventories
		m.inventory = f.inventory
		m.group = f.group
		m.groupPage = f.groups
		m.hostPage = f.hosts
		m.inventoryDetail, m.groupDetail, m.hostDetail = f.inventoryDetail, f.groupDetail, f.hostDetail
		m.nodeDetail, m.objectDetailsFocus = f.nodeDetail, f.objectDetailsFocus
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hostGroupsMsg:
		if msg.id != m.hostGroupsID || msg.hostID != m.hostDetail.ID || m.screen != hostScreen || m.quitting || m.ctx.Err() != nil {
			return m, nil
		}
		m.hostGroupsLoading = false
		m.hostGroupsErr = msg.err
		if msg.err == nil {
			m.hostGroupPage, m.hostGroupsKnown = msg.page, true
		}
	case runningJobsMsg:
		if m.quitting || m.ctx.Err() != nil {
			return m, nil
		}
		m.runningPending = false
		m.runningErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.runningErr = msg.update.Err
		if msg.update.Err == nil {
			m.runningCount, m.runningKnown = msg.update.Count, true
		}
		return m, m.nextRunningJobs()
	case userMsg:
		if !m.quitting && m.ctx.Err() == nil {
			m.userLoading = false
			m.user, m.userErr = msg.user, msg.err
		}
	case tea.BackgroundColorMsg:
		m.styles = newStyles(msg.IsDark())
	case liveMsg:
		if msg.id != m.outputID || m.screen != outputScreen || m.quitting {
			return m, nil
		}
		m.outputPending = false
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = msg.update.Err
		if msg.update.Retrying {
			m.outputUpdate.Retrying = true
			return m, m.nextOutput()
		}
		if m.following {
			m.outputUpdate = msg.update
			m.outputTop = msg.update.Chunk.Start
		} else {
			m.outputUpdate.Cursor = msg.update.Cursor
			m.outputUpdate.Status = msg.update.Status
			m.outputUpdate.Complete = msg.update.Complete
			m.outputUpdate.CompletionKnown = msg.update.CompletionKnown
			m.outputUpdate.Chunk.AbsoluteEnd = msg.update.Chunk.AbsoluteEnd
			m.outputUpdate.Retrying = false
		}
		if !msg.update.Complete {
			return m, m.nextOutput()
		}
	case cleanupMsg:
		if msg.err == nil {
			for i, s := range m.sessions {
				if s == msg.session {
					m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
					break
				}
			}
		}
		if msg.err != nil {
			m.cleanupErr = msg.err
			m.err = msg.err
		}
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 1)
		m.height = max(msg.Height, 1)
	case resultMsg:
		if msg.id != m.generation || m.quitting {
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		if result, ok := msg.value.(aap.CancellationResult); ok && result.Job.Ref.ID > 0 {
			m.jobDetail = result.Job
		}
		if msg.err == nil {
			switch v := msg.value.(type) {
			case templateJobsMsg:
				m.templateJobPage = v.page
				m.selected = 0
				return m, tea.ClearScreen
			case aap.Page[aap.GroupSummary]:
				m.groupPage = v
				m.selected = 0
				return m, tea.ClearScreen
			case aap.Page[aap.HostSummary]:
				m.hostPage = v
				m.selected = 0
				return m, tea.ClearScreen
			case aap.Page[aap.InventorySummary]:
				m.inventoryPage = v
				m.selected = 0
				return m, tea.ClearScreen
			case aap.Page[aap.TemplateSummary]:
				m.templatePage = v
				m.selected = 0
				// Repaint the replacement list instead of reusing terminal rows
				// from the previous search or page.
				return m, tea.ClearScreen
			case aap.Page[aap.JobSummary]:
				m.jobPage = v
				m.selected = 0
				return m, tea.ClearScreen
			case aap.InventoryDetails:
				m.inventoryDetail, m.inventory = v, v.InventorySummary
			case aap.GroupDetails:
				m.groupDetail, m.group = v, v.GroupSummary
			case aap.HostDetails:
				m.hostDetail = v
			case aap.JobDetails:
				m.jobDetail = v
			case aap.Page[aap.WorkflowNode]:
				m.nodePage = v
				m.selected = 0
				return m, tea.ClearScreen
			case aap.OutputUpdate:
				status := m.outputUpdate.Status
				complete := m.outputUpdate.Complete
				known := m.outputUpdate.CompletionKnown
				retrying := m.outputUpdate.Retrying
				m.outputUpdate = v
				m.outputUpdate.Status = status
				m.outputUpdate.Complete = complete
				m.outputUpdate.CompletionKnown = known
				m.outputUpdate.Retrying = retrying
				m.outputTop = v.Chunk.Start
			case aap.TemplateDetails:
				m.templateDetail = v
				if m.hasTemplateJobs() {
					return m, m.loadTemplateJobs(aap.PageCursor{})
				}
			}
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.quitting = true
			m.cancel()
			m.invalidate()
			return m, tea.Quit
		}
		if m.confirming {
			switch key {
			case "y", "enter":
				ref := m.confirmation
				m.confirming = false
				return m, m.begin(func(ctx context.Context) (any, error) { return m.canceller.Cancel(ctx, ref) })
			case "q":
				m.quitting = true
				m.cancel()
				m.invalidate()
				return m, tea.Quit
			case "n", "esc":
				m.confirming = false
			}
			return m, nil
		}
		if m.editing {
			switch key {
			case "esc":
				m.editing = false
			case "enter":
				m.editing = false
				if m.screen == templatesScreen {
					return m, m.loadTemplates(aap.PageCursor{})
				}
				if m.screen == jobsScreen {
					return m, m.loadJobs(aap.PageCursor{})
				}
				if m.screen == projectsScreen {
					return m, m.loadProjects(aap.PageCursor{})
				}
				if m.inventoryList() {
					return m, m.loadInventoryList(aap.PageCursor{})
				}
			case "backspace":
				r := []rune(m.search)
				if len(r) > 0 {
					m.search = string(r[:len(r)-1])
				}
			default:
				if msg.Text != "" && len(m.search) < 256 {
					text := strings.Map(func(r rune) rune {
						if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
							return -1
						}
						return r
					}, msg.Text)
					if len(m.search)+len(text) <= 256 {
						m.search += text
					}
				}
			}
			return m, nil
		}
		switch key {
		case "tab":
			if m.screen == inventoryScreen || m.screen == groupScreen {
				m.objectDetailsFocus = !m.objectDetailsFocus
			}
			if m.hasTemplateJobs() {
				m.templateJobsFocus = !m.templateJobsFocus
			}
		case "q":
			m.quitting = true
			m.cancel()
			m.invalidate()
			return m, tea.Quit
		case "esc":
			cleanup := m.closeOutput()
			previous := m.screen
			m.back()
			if m.screen != previous {
				// Clear rows from the screen being left before repainting the
				// restored frame, including when output cleanup is asynchronous.
				return m, tea.Batch(tea.ClearScreen, cleanup)
			}
			return m, cleanup
		case "/":
			if m.screen == jobsScreen || m.screen == templatesScreen || m.screen == projectsScreen || m.inventoryList() {
				m.editing = true
			}
		case "up", "k":
			if m.scrollingDetails() {
				m.detailTop = max(0, m.detailTop-1)
				return m, nil
			}
			if m.screen == outputScreen && !m.loading {
				if m.following {
					m.outputTop = m.visibleOutputStart()
				}
				m.following = false
				m.outputTop = max(0, m.outputTop-1)
				return m, m.readOutput(m.outputTop)
			}
			m.selected = max(0, m.selected-1)
		case "f":
			if m.screen == outputScreen {
				m.following = true
				start := max(0, m.outputUpdate.Cursor.Line-m.outputLines())
				return m, tea.Batch(m.readOutput(start), m.nextOutput())
			}
		case "r":
			if m.screen == inventoryScreen || m.screen == groupScreen || m.screen == hostScreen {
				return m, m.loadObjectDetails()
			}
			if m.hasTemplateJobs() {
				if m.loading {
					return m, nil
				}
				return m, m.loadTemplateJobs(aap.PageCursor{})
			}
			if m.inventoryList() {
				return m, m.loadInventoryList(aap.PageCursor{})
			}
			if m.screen == outputScreen {
				return m, m.nextOutput()
			}
			if m.screen == templatesScreen {
				return m, m.loadTemplates(aap.PageCursor{})
			}
			if m.screen == projectsScreen {
				return m, m.loadProjects(aap.PageCursor{})
			}
			if m.screen == jobsScreen {
				return m, m.loadJobs(aap.PageCursor{})
			}
			if m.screen == jobScreen {
				return m, m.loadJob(m.jobDetail.Ref)
			}
			if m.screen == workflowScreen {
				return m, m.loadNodes(aap.PageCursor{})
			}
		case "d":
			if m.screen == workflowScreen && !m.loading && m.selected < len(m.nodePage.Items) {
				node := m.nodePage.Items[m.selected]
				m.move(nodeScreen)
				m.nodeDetail = node
			}
		case "c":
			if m.screen == jobScreen && !m.loading && m.canceller != nil {
				if m.jobDetail.Capabilities.Cancel.State == aap.Available {
					m.confirming = true
					m.confirmation = m.jobDetail.Ref
				} else {
					m.err = fmt.Errorf("cancel: %s", m.jobDetail.Capabilities.Cancel.Reason)
				}
			}
		case "o":
			if m.screen == jobScreen && !m.loading {
				if m.jobDetail.Capabilities.Output.State != aap.Available {
					m.err = fmt.Errorf("output: %s", m.jobDetail.Capabilities.Output.Reason)
				} else if m.openOutput != nil {
					m.move(outputScreen)
					m.output = m.openOutput(m.ctx, m.jobDetail.Ref)
					m.sessions = append(m.sessions, m.output)
					m.outputTop = 0
					m.outputLeft = 0
					m.following = true
					m.outputID++
					m.outputPending = false
					m.outputUpdate = aap.OutputUpdate{}
					return m, m.nextOutput()
				}
			}
		case "w":
			if m.screen == jobScreen && !m.loading && m.jobDetail.Capabilities.Children.State == aap.Available {
				m.move(workflowScreen)
				return m, m.loadNodes(aap.PageCursor{})
			}
		case "n":
			if m.screen == hostScreen && !m.hostGroupsLoading && m.hostGroupPage.Next.Present() {
				return m, m.loadHostGroups(m.hostGroupPage.Next)
			}
			if m.hasTemplateJobs() && m.templateJobPage.Next.Present() {
				return m, m.loadTemplateJobs(m.templateJobPage.Next)
			}
			if m.inventoryList() && m.inventoryCursor(true).Present() {
				return m, m.loadInventoryList(m.inventoryCursor(true))
			}
			if m.screen == projectsScreen && m.templatePage.Next.Present() {
				return m, m.loadProjects(m.templatePage.Next)
			}
			if m.screen == templatesScreen && m.templatePage.Next.Present() {
				return m, m.loadTemplates(m.templatePage.Next)
			}
			if m.screen == jobsScreen && m.jobPage.Next.Present() {
				return m, m.loadJobs(m.jobPage.Next)
			}
			if m.screen == workflowScreen && m.nodePage.Next.Present() {
				return m, m.loadNodes(m.nodePage.Next)
			}
		case "p":
			if m.screen == hostScreen && !m.hostGroupsLoading && m.hostGroupPage.Previous.Present() {
				return m, m.loadHostGroups(m.hostGroupPage.Previous)
			}
			if m.hasTemplateJobs() && m.templateJobPage.Previous.Present() {
				return m, m.loadTemplateJobs(m.templateJobPage.Previous)
			}
			if m.inventoryList() && m.inventoryCursor(false).Present() {
				return m, m.loadInventoryList(m.inventoryCursor(false))
			}
			if m.screen == projectsScreen && m.templatePage.Previous.Present() {
				return m, m.loadProjects(m.templatePage.Previous)
			}
			if m.screen == templatesScreen && m.templatePage.Previous.Present() {
				return m, m.loadTemplates(m.templatePage.Previous)
			}
			if m.screen == jobsScreen && m.jobPage.Previous.Present() {
				return m, m.loadJobs(m.jobPage.Previous)
			}
			if m.screen == workflowScreen && m.nodePage.Previous.Present() {
				return m, m.loadNodes(m.nodePage.Previous)
			}
		case "left", "h":
			if m.screen == outputScreen {
				m.outputLeft = max(0, m.outputLeft-8)
			}
		case "right", "l":
			if m.screen == outputScreen {
				m.outputLeft = min(aap.OutputBytes, m.outputLeft+8)
			}
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			if m.screen == outputScreen && !m.loading {
				if m.following {
					m.outputTop = m.visibleOutputStart()
				}
				m.following = false
				delta := m.outputLines()
				if key == "pgup" || key == "ctrl+u" {
					delta = -delta
				}
				m.outputTop = max(0, min(max(m.outputUpdate.Chunk.AbsoluteEnd-1, 0), m.outputTop+delta))
				return m, m.readOutput(m.outputTop)
			}
		case "down", "j":
			if m.scrollingDetails() {
				m.detailTop = min(1024*1024, m.detailTop+1)
				return m, nil
			}
			if m.screen == outputScreen && !m.loading {
				if m.following {
					m.outputTop = m.visibleOutputStart()
				}
				m.following = false
				m.outputTop = min(max(m.outputUpdate.Chunk.AbsoluteEnd-1, 0), m.outputTop+1)
				return m, m.readOutput(m.outputTop)
			}
			if m.screen == mainScreen {
				m.selected = min(3, m.selected+1)
			} else if m.screen == templatesScreen || m.screen == projectsScreen {
				m.selected = min(max(len(m.templatePage.Items)-1, 0), m.selected+1)
			}
			if m.screen == jobsScreen {
				m.selected = min(max(len(m.jobPage.Items)-1, 0), m.selected+1)
			}
			if m.hasTemplateJobs() {
				m.selected = min(max(len(m.templateJobPage.Items)-1, 0), m.selected+1)
			}
			if m.screen == inventoriesScreen {
				m.selected = min(max(len(m.inventoryPage.Items)-1, 0), m.selected+1)
			}
			if m.screen == inventoryScreen || m.screen == groupScreen {
				m.selected = min(1, m.selected+1)
			}
			if m.screen == groupsScreen {
				m.selected = min(max(len(m.groupPage.Items)-1, 0), m.selected+1)
			}
			if m.screen == hostsScreen {
				m.selected = min(max(len(m.hostPage.Items)-1, 0), m.selected+1)
			}
			if m.screen == workflowScreen {
				m.selected = min(max(len(m.nodePage.Items)-1, 0), m.selected+1)
			}
		case "enter":
			if m.screen == nodeScreen && m.nodeDetail.Job != nil && !m.loading {
				ref := *m.nodeDetail.Job
				m.move(jobScreen)
				return m, m.loadJob(ref)
			}
			if m.screen == hostsScreen && !m.loading && m.selected < len(m.hostPage.Items) {
				host := m.hostPage.Items[m.selected]
				m.move(hostScreen)
				m.hostDetail = aap.HostDetails{HostSummary: host, Inventory: &aap.ResourceReference{ID: host.InventoryID}}
				return m, m.loadObjectDetails()
			}
			if m.hasTemplateJobs() && m.templateJobsFocus && !m.loading && m.selected < len(m.templateJobPage.Items) {
				ref := m.templateJobPage.Items[m.selected].Ref
				m.move(jobScreen)
				return m, m.loadJob(ref)
			}
			if m.screen == inventoriesScreen && !m.loading && m.selected < len(m.inventoryPage.Items) {
				inventory := m.inventoryPage.Items[m.selected]
				m.move(inventoryScreen)
				m.inventory = inventory
				m.group = aap.GroupSummary{}
				m.inventoryDetail = aap.InventoryDetails{InventorySummary: inventory}
				return m, m.loadObjectDetails()
			}
			if m.screen == groupsScreen && !m.loading && m.selected < len(m.groupPage.Items) {
				group := m.groupPage.Items[m.selected]
				m.move(groupScreen)
				m.group = group
				m.groupDetail = aap.GroupDetails{GroupSummary: group, Inventory: &aap.ResourceReference{ID: group.InventoryID}}
				return m, m.loadObjectDetails()
			}
			if (m.screen == inventoryScreen || m.screen == groupScreen) && !m.objectDetailsFocus && !m.loading && m.inventoryContents != nil {
				s := groupsScreen
				if m.selected == 1 {
					s = hostsScreen
				}
				m.move(s)
				m.groupPage = aap.Page[aap.GroupSummary]{}
				m.hostPage = aap.Page[aap.HostSummary]{}
				return m, m.loadInventoryList(aap.PageCursor{})
			}
			if m.screen == mainScreen {
				switch m.selected {
				case 0:
					m.move(templatesScreen)
					m.templatePage = aap.Page[aap.TemplateSummary]{}
					return m, m.loadTemplates(aap.PageCursor{})
				case 1:
					m.move(jobsScreen)
					return m, m.loadJobs(aap.PageCursor{})
				case 2:
					m.move(projectsScreen)
					m.templatePage = aap.Page[aap.TemplateSummary]{}
					return m, m.loadProjects(aap.PageCursor{})
				case 3:
					m.move(inventoriesScreen)
					m.inventoryPage = aap.Page[aap.InventorySummary]{}
					return m, m.loadInventories(aap.PageCursor{})
				}
			} else if m.screen == projectsScreen && !m.loading && m.projects != nil && m.selected < len(m.templatePage.Items) {
				summary := m.templatePage.Items[m.selected]
				ref := summary.Ref
				m.move(templateScreen)
				m.templateDetail = aap.TemplateDetails{TemplateSummary: summary}
				return m, m.begin(func(ctx context.Context) (any, error) { return m.projects.Template(ctx, ref) })
			} else if m.screen == templatesScreen && !m.loading && m.templates != nil && m.selected < len(m.templatePage.Items) {
				summary := m.templatePage.Items[m.selected]
				ref := summary.Ref
				m.move(templateScreen)
				m.templateDetail = aap.TemplateDetails{TemplateSummary: summary}
				m.templateJobPage = aap.Page[aap.JobSummary]{}
				m.templateJobsFocus = true
				return m, m.begin(func(ctx context.Context) (any, error) { return m.templates.Template(ctx, ref) })
			} else if m.screen == jobsScreen && !m.loading && m.selected < len(m.jobPage.Items) {
				ref := m.jobPage.Items[m.selected].Ref
				m.move(jobScreen)
				return m, m.loadJob(ref)
			} else if m.screen == workflowScreen && !m.loading && m.selected < len(m.nodePage.Items) {
				node := m.nodePage.Items[m.selected]
				if node.Job != nil {
					ref := *node.Job
					m.move(jobScreen)
					return m, m.loadJob(ref)
				}
			}
		}
	}
	return m, nil
}
