package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/leehinman/aascan/api"
	"github.com/leehinman/aascan/download"
)

type appState int

const (
	stateVersions appState = iota
	stateProjects
	statePackages
	stateDownloading
)

// Messages

type versionsLoadedMsg struct {
	versions []string
	err      error
}

type projectsLoadedMsg struct {
	projects map[string]map[string]api.Package
	err      error
}

type downloadProgressMsg download.Progress

// List item types

type versionItem struct{ version string }

func (i versionItem) Title() string       { return i.version }
func (i versionItem) Description() string { return "" }
func (i versionItem) FilterValue() string { return i.version }

type projectItem struct{ name string }

func (i projectItem) Title() string       { return i.name }
func (i projectItem) Description() string { return "" }
func (i projectItem) FilterValue() string { return i.name }

type packageItem struct{ pkg api.Package }

func (i packageItem) Title() string { return i.pkg.Name }
func (i packageItem) Description() string {
	var parts []string
	if i.pkg.Type != "" {
		parts = append(parts, i.pkg.Type)
	}
	if i.pkg.Architecture != "" {
		parts = append(parts, i.pkg.Architecture)
	}
	if len(i.pkg.OS) > 0 {
		parts = append(parts, strings.Join(i.pkg.OS, "/"))
	}
	if i.pkg.Classifier != "" {
		parts = append(parts, i.pkg.Classifier)
	}
	return strings.Join(parts, " · ")
}
func (i packageItem) FilterValue() string { return i.pkg.Name }

// Styles

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62")).Padding(0, 1)
	crumbStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
)

// Model

type Model struct {
	state   appState
	loading bool
	err     error
	width   int
	height  int

	client          *api.Client
	selectedVersion string
	selectedProject string
	projectData     map[string]map[string]api.Package

	versionList list.Model
	projectList list.Model
	packageList list.Model

	spinner     spinner.Model
	prog        progress.Model
	progressCh  chan download.Progress
	dlProgress  download.Progress
	statusMsg   string
	statusIsErr bool
}

func newList(width, height int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	l := list.New(nil, delegate, width, height)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle.Width(width)
	return l
}

type twoColDelegate struct {
	styles list.DefaultItemStyles
}

func (d twoColDelegate) Height() int                               { return 1 }
func (d twoColDelegate) Spacing() int                              { return 0 }
func (d twoColDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd  { return nil }
func (d twoColDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	pkg, ok := item.(packageItem)
	if !ok || m.Width() <= 0 {
		return
	}

	// 2 chars consumed by the left indicator (border or padding)
	const indWidth = 2
	avail := m.Width() - indWidth
	if avail <= 2 {
		return
	}
	col1 := avail * 3 / 5
	col2 := avail - col1 - 1 // -1 for gap

	name := ansi.Truncate(pkg.Title(), col1, "…")
	desc := ansi.Truncate(pkg.Description(), col2, "…")
	// pad name to fill col1 so description aligns
	if pad := col1 - lipgloss.Width(name); pad > 0 {
		name += strings.Repeat(" ", pad)
	}

	isSelected := index == m.Index()
	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""

	var ind string
	var nameStyle, descStyle lipgloss.Style
	indColor := lipgloss.AdaptiveColor{Light: "#F793FF", Dark: "#AD58B4"}

	switch {
	case emptyFilter:
		ind = "  "
		nameStyle = d.styles.DimmedTitle.Inline(true)
		descStyle = d.styles.DimmedDesc.Inline(true)
	case isSelected && m.FilterState() != list.Filtering:
		ind = lipgloss.NewStyle().Foreground(indColor).Render("│") + " "
		nameStyle = d.styles.SelectedTitle.Inline(true)
		descStyle = d.styles.SelectedDesc.Inline(true)
	default:
		ind = "  "
		nameStyle = d.styles.NormalTitle.Inline(true)
		descStyle = d.styles.NormalDesc.Inline(true)
	}

	fmt.Fprintf(w, "%s%s %s", ind, nameStyle.Render(name), descStyle.Render(desc))
}

func newPackageList(width, height int) list.Model {
	l := list.New(nil, twoColDelegate{styles: list.NewDefaultItemStyles()}, width, height)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle.Width(width)
	return l
}

func NewModel(client *api.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	p := progress.New(progress.WithDefaultGradient())

	vl := newList(0, 0)
	vl.Title = "Versions"
	pl := newList(0, 0)
	pl.Title = "Projects"
	pkl := newPackageList(0, 0)
	pkl.Title = "Packages"

	return Model{
		state:       stateVersions,
		loading:     true,
		client:      client,
		spinner:     s,
		prog:        p,
		versionList: vl,
		projectList: pl,
		packageList: pkl,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.cmdFetchVersions())
}

func (m Model) cmdFetchVersions() tea.Cmd {
	return func() tea.Msg {
		versions, err := m.client.GetVersions()
		return versionsLoadedMsg{versions: versions, err: err}
	}
}

func (m Model) cmdFetchProjects(version string) tea.Cmd {
	return func() tea.Msg {
		projects, err := m.client.GetProjects(version)
		return projectsLoadedMsg{projects: projects, err: err}
	}
}

func listenForProgress(ch chan download.Progress) tea.Cmd {
	return func() tea.Msg { return downloadProgressMsg(<-ch) }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h := max(msg.Height-8, 5)
		m.versionList.SetSize(msg.Width, h)
		m.versionList.Styles.Title = titleStyle.Width(msg.Width)
		m.projectList.SetSize(msg.Width, h)
		m.projectList.Styles.Title = titleStyle.Width(msg.Width)
		m.packageList.SetSize(msg.Width, h)
		m.packageList.Styles.Title = titleStyle.Width(msg.Width)
		m.prog.Width = max(msg.Width-4, 10)
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case versionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		versions := msg.versions
		for i, j := 0, len(versions)-1; i < j; i, j = i+1, j-1 {
			versions[i], versions[j] = versions[j], versions[i]
		}
		items := make([]list.Item, len(versions))
		for i, v := range versions {
			items[i] = versionItem{version: v}
		}
		h := max(m.height-8, 5)
		m.versionList = newList(m.width, h)
		m.versionList.Title = "Versions"
		m.versionList.Styles.Title = titleStyle.Width(m.width)
		m.versionList.SetItems(items)
		return m, nil

	case projectsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.state = stateVersions
			return m, nil
		}
		m.projectData = msg.projects
		names := make([]string, 0, len(msg.projects))
		for name := range msg.projects {
			names = append(names, name)
		}
		sort.Strings(names)
		items := make([]list.Item, len(names))
		for i, name := range names {
			items[i] = projectItem{name: name}
		}
		h := max(m.height-8, 5)
		m.projectList = newList(m.width, h)
		m.projectList.Title = "Projects"
		m.projectList.Styles.Title = titleStyle.Width(m.width)
		m.projectList.SetItems(items)
		m.state = stateProjects
		return m, nil

	case downloadProgressMsg:
		m.dlProgress = download.Progress(msg)
		if msg.Err != nil {
			m.state = statePackages
			m.statusMsg = "Download failed: " + msg.Err.Error()
			m.statusIsErr = true
			return m, nil
		}
		if msg.Done {
			m.state = statePackages
			m.statusMsg = "Saved to " + msg.Path
			m.statusIsErr = false
			return m, nil
		}
		var progCmd tea.Cmd
		if msg.Total > 0 {
			progCmd = m.prog.SetPercent(float64(msg.Downloaded) / float64(msg.Total))
		}
		return m, tea.Batch(progCmd, listenForProgress(m.progressCh))

	case progress.FrameMsg:
		pm, cmd := m.prog.Update(msg)
		m.prog = pm.(progress.Model)
		return m, cmd
	}

	// Per-state handling
	switch m.state {
	case stateVersions:
		return m.updateVersions(msg)
	case stateProjects:
		return m.updateProjects(msg)
	case statePackages:
		return m.updatePackages(msg)
	case stateDownloading:
		// No interaction while downloading
	}
	return m, nil
}

func (m Model) updateVersions(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	prevFilter := m.versionList.FilterState()
	var listCmd tea.Cmd
	m.versionList, listCmd = m.versionList.Update(msg)

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if prevFilter == list.Unfiltered {
				return m, tea.Quit
			}
		case "enter":
			if prevFilter != list.Filtering {
				if item, ok := m.versionList.SelectedItem().(versionItem); ok {
					m.selectedVersion = item.version
					m.loading = true
					m.state = stateProjects
					return m, tea.Batch(listCmd, m.spinner.Tick, m.cmdFetchProjects(item.version))
				}
			}
		}
	}
	return m, listCmd
}

func (m Model) updateProjects(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	prevFilter := m.projectList.FilterState()
	var listCmd tea.Cmd
	m.projectList, listCmd = m.projectList.Update(msg)

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if prevFilter == list.Unfiltered {
				m.state = stateVersions
				m.selectedVersion = ""
				return m, nil
			}
		case "esc":
			if prevFilter == list.Unfiltered {
				m.state = stateVersions
				m.selectedVersion = ""
				return m, nil
			}
		case "enter":
			if prevFilter != list.Filtering {
				if item, ok := m.projectList.SelectedItem().(projectItem); ok {
					m.selectedProject = item.name
					pkgs := m.projectData[item.name]
					items := make([]list.Item, 0, len(pkgs))
					for _, pkg := range pkgs {
						items = append(items, packageItem{pkg: pkg})
					}
					sort.Slice(items, func(i, j int) bool {
						return items[i].(packageItem).pkg.Name < items[j].(packageItem).pkg.Name
					})
					h := max(m.height-8, 5)
					m.packageList = newPackageList(m.width, h)
					m.packageList.Title = "Packages"
					m.packageList.Styles.Title = titleStyle.Width(m.width)
					m.packageList.SetItems(items)
					m.statusMsg = ""
					m.state = statePackages
					return m, listCmd
				}
			}
		}
	}
	return m, listCmd
}

func (m Model) updatePackages(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevFilter := m.packageList.FilterState()
	var listCmd tea.Cmd
	m.packageList, listCmd = m.packageList.Update(msg)

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if prevFilter == list.Unfiltered {
				m.state = stateProjects
				m.selectedProject = ""
				m.statusMsg = ""
				return m, nil
			}
		case "esc":
			if prevFilter == list.Unfiltered {
				m.state = stateProjects
				m.selectedProject = ""
				m.statusMsg = ""
				return m, nil
			}
		case "enter":
			if prevFilter != list.Filtering {
				if item, ok := m.packageList.SelectedItem().(packageItem); ok && item.pkg.URL != "" {
					ch := make(chan download.Progress, 20)
					m.progressCh = ch
					m.dlProgress = download.Progress{}
					m.state = stateDownloading
					m.statusMsg = ""
					go download.Download(item.pkg.URL, item.pkg.SHAURL, ch)
					return m, tea.Batch(
						listCmd,
						m.prog.SetPercent(0),
						listenForProgress(ch),
					)
				}
			}
		}
	}
	return m, listCmd
}

func (m Model) View() string {
	var b strings.Builder

	// Breadcrumb header
	crumb := "aascan"
	if m.selectedVersion != "" {
		crumb += " › " + m.selectedVersion
	}
	if m.selectedProject != "" {
		crumb += " › " + m.selectedProject
	}
	b.WriteString(crumbStyle.Render(crumb))
	b.WriteByte('\n')

	if m.err != nil && m.state == stateVersions {
		b.WriteString(errStyle.Render("\nerror: "+m.err.Error()) + "\n\npress q to quit\n")
		return b.String()
	}

	if m.loading {
		b.WriteString("\n" + m.spinner.View() + " loading...\n")
		return b.String()
	}

	switch m.state {
	case stateVersions:
		b.WriteString(m.versionList.View())
	case stateProjects:
		b.WriteString(m.projectList.View())
	case statePackages:
		b.WriteString(m.packageList.View())
		if m.statusMsg != "" {
			b.WriteByte('\n')
			if m.statusIsErr {
				b.WriteString(errStyle.Render(m.statusMsg))
			} else {
				b.WriteString(successStyle.Render(m.statusMsg))
			}
		}
	case stateDownloading:
		var pkgName string
		if item, ok := m.packageList.SelectedItem().(packageItem); ok {
			pkgName = item.pkg.Name
		}
		b.WriteString("\ndownloading " + pkgName + "\n\n")
		b.WriteString(m.prog.View())
		b.WriteByte('\n')
		if m.dlProgress.Total > 0 {
			b.WriteString(helpStyle.Render(fmt.Sprintf(
				"%s / %s",
				humanBytes(m.dlProgress.Downloaded),
				humanBytes(m.dlProgress.Total),
			)))
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
