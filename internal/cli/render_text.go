package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

// textPage collects one human-readable result and writes it once.
type textPage struct{ strings.Builder }

func (p *textPage) line(format string, args ...any) { fmt.Fprintf(p, format+"\n", args...) }

// field writes "Label: value" when value is nonempty.
func (p *textPage) field(label, value string) {
	if value != "" {
		p.line("%s: %s", label, value)
	}
}

// table writes a heading and indented, tab-aligned rows, or nothing without rows.
func (p *textPage) table(heading string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	p.line("\n%s:", heading)
	p.grid("  ", rows)
}

func (p *textPage) grid(indent string, rows [][]string) {
	tw := tabwriter.NewWriter(p, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(tw, indent+strings.Join(row, "\t"))
	}
	tw.Flush()
}

// flush escapes control characters other than newline and tab, so stored
// text such as a document body cannot send terminal escape sequences.
func (p *textPage) flush(w io.Writer) error {
	var b strings.Builder
	for _, r := range p.String() {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			fmt.Fprintf(&b, `\x%02x`, r) // IsControl runes are all below U+00A0.
			continue
		}
		b.WriteRune(r)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func dash(purpose string) string {
	if purpose == "" {
		return ""
	}
	return "— " + purpose
}

func flag(v *bool) string {
	switch {
	case v == nil:
		return "not checked"
	case *v:
		return "yes"
	default:
		return "no"
	}
}

func renderContext(w io.Writer, r app.ContextResult) error {
	var p textPage
	if d := r.Document; d != nil {
		p.line("%s (%s)", oneLine(d.Title), d.ID)
		p.field("Purpose", oneLine(d.Purpose))
		p.field("Source", oneLine(d.Source))
		p.line("Policy: %s, %s disclosure", d.Policy.Storage, d.Policy.AgentDisclosure)
		p.line("Revision: %s (updated %s, freshness %s)", d.Revision, d.UpdatedAt, d.Freshness)
		if d.Summary != nil && *d.Summary != "" {
			p.line("Summary: %s", oneLine(*d.Summary))
		}
		if d.Body != nil {
			p.line("\n%s", strings.TrimRight(*d.Body, "\n"))
		}
		return p.flush(w)
	}
	p.line("Project: %s (%s)", r.Project.Slug, r.Project.ID)
	if r.Summary != nil {
		p.line("Summary: %s", oneLine(*r.Summary))
	}
	if r.Profile != nil {
		p.line("Profile: %s", r.Profile.Name)
	}
	if e := r.Environment; e != nil {
		p.line("Environment: %s/%s on device %s", e.OS, e.Arch, e.DeviceID)
	}
	if r.LastSyncAt != nil {
		p.line("Last sync: %s", *r.LastSyncAt)
	}
	docs := [][]string{}
	for _, d := range r.Documents {
		title := oneLine(d.Title)
		if d.Freshness == "stale" {
			title += " (stale)"
		}
		policy := d.Policy.Storage + "/" + d.Policy.AgentDisclosure
		docs = append(docs, []string{d.ID, title, policy, dash(oneLine(d.Purpose))})
	}
	p.table("Documents", docs)
	resources := [][]string{}
	for _, res := range r.Resources {
		resources = append(resources, []string{res.ID, res.Identity, dash(res.Purpose)})
	}
	p.table("Resources", resources)
	tasks := [][]string{}
	for _, t := range r.Tasks {
		tasks = append(tasks, []string{t.ID, t.Name, dash(t.Purpose)})
	}
	p.table("Tasks", tasks)
	if len(docs)+len(resources)+len(tasks) == 0 {
		p.line("\nNo documents, Resources or Tasks.")
	}
	if r.Truncated {
		p.line("\nMore entries: --cursor %s", r.NextCursor)
	}
	return p.flush(w)
}

var safeToken = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellWord shows one program or argument unambiguously: anything but a plain
// token is quoted with every control character escaped, so a reviewed command
// line can neither split differently nor redraw the terminal.
func shellWord(s string) string {
	if safeToken.MatchString(s) {
		return s
	}
	return strconv.Quote(s)
}

// oneLine quotes a value that would otherwise break or redraw its line.
func oneLine(s string) string {
	if strings.ContainsFunc(s, unicode.IsControl) {
		return strconv.Quote(s)
	}
	return s
}

func taskCommandLine(t app.TaskSpec) string {
	if t.Program == "" {
		return "(none; exec only)"
	}
	parts := []string{shellWord(t.Program)}
	for _, arg := range t.Argv {
		parts = append(parts, shellWord(arg))
	}
	return strings.Join(parts, " ")
}

func renderTask(w io.Writer, t app.Task) error {
	var p textPage
	p.line("%s (%s)", t.Name, t.ID)
	p.field("Purpose", t.Purpose)
	p.line("Command: %s", taskCommandLine(t.TaskSpec))
	p.line("Working directory: %s (membership %s)", t.WorkingDirectory, t.MembershipID)
	p.line("Storage: %s", t.Policy.Storage)
	p.line("Revision: %s", t.Revision)
	if len(t.Platforms) > 0 {
		p.line("Platforms: %s", strings.Join(t.Platforms, ", "))
	}
	if t.AllowCustomCommand {
		p.line("Custom commands: allowed with exec")
	}
	for _, input := range t.Inputs {
		p.line("Input: %s", input)
	}
	slots := [][]string{}
	for _, s := range t.ResourceSlots {
		slots = append(slots, []string{s.Name, s.Kind, s.Stage})
	}
	p.table("Resource slots", slots)
	creds := [][]string{}
	for _, c := range t.CredentialRequirements {
		slot := "standalone"
		if c.ResourceSlot != "" {
			slot = "slot " + c.ResourceSlot
		}
		creds = append(creds, []string{c.Env, c.Name, slot, c.Type, dash(c.Purpose)})
	}
	p.table("Credentials", creds)
	return p.flush(w)
}

func renderTaskList(w io.Writer, r app.TaskList) error {
	var p textPage
	if len(r.Tasks) == 0 {
		p.line("No Tasks in %s.", r.Project.Slug)
		return p.flush(w)
	}
	rows := [][]string{{"NAME", "UUID", "COMMAND", "CREDENTIALS"}}
	for _, t := range r.Tasks {
		creds := ""
		if n := len(t.CredentialRequirements); n > 0 {
			creds = fmt.Sprintf("%d credential(s)", n)
		}
		rows = append(rows, []string{t.Name, t.ID, taskCommandLine(t.TaskSpec), creds})
	}
	p.grid("", rows)
	return p.flush(w)
}

func renderResource(p *textPage, r app.Resource) {
	p.line("%s (%s)", r.Identity, r.ID)
	p.field("Purpose", r.Purpose)
	p.line("Kind: %s, stage %s", r.Kind, r.Stage)
	p.line("Storage: %s", r.Policy.Storage)
	if len(r.Endpoints) > 0 {
		p.line("Endpoints: %s", strings.Join(r.Endpoints, ", "))
	}
	p.field("Network", r.Network)
	if len(r.Platforms) > 0 {
		p.line("Platforms: %s", strings.Join(r.Platforms, ", "))
	}
}

func renderResourceList(w io.Writer, r app.ResourceList) error {
	var p textPage
	if len(r.Resources) == 0 {
		p.line("No Resources in %s.", r.Project.Slug)
		return p.flush(w)
	}
	rows := [][]string{{"IDENTITY", "UUID", "KIND/STAGE", "STORAGE", "PURPOSE"}}
	for _, res := range r.Resources {
		rows = append(rows, []string{res.Identity, res.ID, res.Kind + "/" + res.Stage, res.Policy.Storage, res.Purpose})
	}
	p.grid("", rows)
	return p.flush(w)
}

func observationLine(o app.Observation) string {
	return fmt.Sprintf("%s %s %s (%s)", o.ObservedAt, o.Status, o.Scope.Target, o.Tool)
}

func renderResourceDetail(w io.Writer, r app.ResourceDetail) error {
	var p textPage
	renderResource(&p, r.Resource)
	route := "Route: " + r.Route.Status
	if r.Route.Endpoint != "" {
		route += " " + r.Route.Endpoint
	}
	if r.Route.Proxy != "" {
		route += " via proxy " + r.Route.Proxy
	}
	if r.Route.Source != "" {
		route += " (" + r.Route.Source + ")"
	}
	p.line("%s", route)
	if len(r.Observations) > 0 {
		p.line("Last check: %s", observationLine(r.Observations[0]))
	}
	return p.flush(w)
}

func providerLabel(pp provider.Profile) string {
	return fmt.Sprintf("%s/%s (%s)", pp.Provider, pp.Name, pp.Method)
}

func locatorLabel(l provider.Locator) string {
	if l.EntryID != "" {
		return "entry " + l.EntryID
	}
	return fmt.Sprintf("%s:%s%s#%s", l.ProjectID, l.Environment, l.Path, l.Key)
}

func renderSecretList(w io.Writer, r app.SecretList) error {
	var p textPage
	if len(r.Bindings) == 0 {
		p.line("No SecretBindings in %s.", r.Project.Slug)
		return p.flush(w)
	}
	rows := [][]string{{"ALIAS", "UUID", "PROVIDER", "LOCATOR", "TYPE"}}
	for _, b := range r.Bindings {
		rows = append(rows, []string{b.Alias, b.ID, providerLabel(b.ProviderProfile), locatorLabel(b.Locator), b.Type})
	}
	p.grid("", rows)
	return p.flush(w)
}

func renderSecretStatus(w io.Writer, r app.SecretStatus) error {
	var p textPage
	p.line("%s / %s (Profile %s)", r.Task, r.Requirement, r.Profile)
	if !r.Bound || r.Binding == nil {
		p.line("Bound: no — map one with ctxferry secret bind %s %s", r.Task, r.Requirement)
		return p.flush(w)
	}
	p.line("Bound: %s via %s, %s", r.Binding.Alias, providerLabel(r.Binding.ProviderProfile), locatorLabel(r.Binding.Locator))
	auth := flag(r.Authenticated)
	if r.Authentication != nil && r.Authentication.State != "" {
		auth += " (" + string(r.Authentication.State) + ")"
	}
	p.line("Provider authenticated: %s", auth)
	p.line("Entry retrievable: %s", flag(r.EntryRetrievable))
	if r.ResourceSlot == "" {
		p.line("Resource reachable: not applicable (standalone credential)")
		return p.flush(w)
	}
	reachable := flag(r.ResourceReachable)
	if r.ReachabilitySeenAt != "" {
		reachable += " (observed " + r.ReachabilitySeenAt + ")"
	}
	p.line("Resource reachable: %s", reachable)
	return p.flush(w)
}

func renderSecretBind(w io.Writer, r app.SecretBindResult) error {
	var p textPage
	p.line("Profile %s maps requirement %s of Task %s to binding %s", r.Profile, r.Mapping.Requirement, r.Mapping.TaskID, r.Mapping.BindingID)
	if b := r.Binding; b != nil {
		p.line("Binding: %s via %s, %s", b.Alias, providerLabel(b.ProviderProfile), locatorLabel(b.Locator))
	}
	return p.flush(w)
}

func renderProviderLogin(w io.Writer, r app.ProviderLoginResult) error {
	var p textPage
	p.line("%s: %s", providerLabel(r.Profile), r.Authentication.State)
	if r.Authentication.ExpiresAt != nil {
		p.line("Expires: %s", r.Authentication.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	return p.flush(w)
}

func renderExecutionReview(w io.Writer, r app.ExecutionReview) error {
	var p textPage
	p.line("%s (%s): %s", r.Task, r.TaskID, r.State)
	p.field("Profile", r.Profile)
	p.field("Enabled at", r.EnabledAt)
	if d := r.Definition; d != nil {
		p.line("Command: %s", taskCommandLine(app.TaskSpec{Program: d.Program, Argv: d.Argv}))
		p.line("Checkout: %s", oneLine(d.CheckoutRoot))
		p.line("Membership: %s (Repository %s, root %s)", d.MembershipID, d.RepositoryID, oneLine(d.MembershipRoot))
		p.line("Working directory: %s", oneLine(d.WorkingDirectory))
		p.line("Custom commands: %t", d.AllowCustomCommand)
		p.field("Profile UUID", d.ProfileID)
		// Every reviewed field of each delivery, so a first approval shows the
		// whole mapping before an enable token is used.
		for _, c := range d.Credentials {
			p.line("\nCredential %s -> %s", c.Requirement, c.Env)
			p.line("  Purpose: %s, type %s", c.Purpose, c.Type)
			if c.ResourceSlot == "" {
				p.line("  Standalone: no Resource")
			} else {
				p.line("  Resource slot %s -> Resource %s (%s)", c.ResourceSlot, c.ResourceID, c.ResourcePurpose)
			}
			p.line("  SecretBinding %s via %s, %s", c.BindingID, providerLabel(c.ProviderProfile), locatorLabel(c.Locator))
		}
	}
	if r.CustomCommands {
		p.line("\nWarning: exec may run any caller-chosen program with these credentials.")
	}
	changes := [][]string{}
	for _, c := range r.Changes {
		changes = append(changes, []string{c.Field, string(c.Enabled), "->", string(c.Current)})
	}
	p.table("Changed since enabled", changes)
	if r.Preview != "" && r.State != "enabled" {
		p.line("\nPreview token: %s", r.Preview)
		p.line("Enable with ctxferry task trust enable %s --preview %s, repeating any --profile or --checkout given here.", r.Task, r.Preview)
	}
	return p.flush(w)
}

func renderObservation(w io.Writer, o app.Observation) error {
	var p textPage
	p.line("%s", observationLine(o))
	return p.flush(w)
}

func renderResourceMapping(w io.Writer, m app.ResourceMapping) error {
	var p textPage
	p.line("Task %s slot %s -> Resource %s", m.TaskID, m.Slot, m.ResourceID)
	return p.flush(w)
}
