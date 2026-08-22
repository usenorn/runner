package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

type Inspection struct {
	client      *control.Client
	out         io.Writer
	in          io.Reader
	interactive func() bool
}

func NewInspection(client *control.Client) *Inspection {
	return &Inspection{
		client:      client,
		out:         os.Stdout,
		in:          os.Stdin,
		interactive: onATerminal,
	}
}

type InspectOptions struct {
	Root    string
	Confirm bool
	JSON    bool
}

func (i *Inspection) Inspect(ctx context.Context, options InspectOptions) error {
	scan, err := i.client.Inspect(ctx, options.Root)
	if err != nil {
		return err
	}

	if options.JSON {
		encoder := json.NewEncoder(i.out)
		encoder.SetIndent("", "  ")

		return encoder.Encode(scan)
	}

	if err := i.report(scan); err != nil {
		return err
	}

	asked, err := i.asked(scan, options.Confirm)
	if err != nil || !asked {
		return err
	}

	accepted, err := i.client.Accept(ctx, scan)
	if err != nil {
		return err
	}

	return i.line(fmt.Sprintf(
		"connected %s to %s with %d repositories",
		accepted.Name, accepted.Server, accepted.Repositories,
	))
}

func (i *Inspection) asked(scan control.Scan, confirmed bool) (bool, error) {
	switch {
	case !scan.Connected:
		return i.confirm(fmt.Sprintf("connect this folder as %q?", scan.Inventory.Name), confirmed)
	case scan.Drift.Any():
		return i.confirm("accept this folder as it now stands?", confirmed)
	case scan.Reconcile:
		return true, nil
	default:
		return false, i.line("nothing has changed since this folder was confirmed")
	}
}

func (i *Inspection) confirm(question string, confirmed bool) (bool, error) {
	if confirmed {
		return true, nil
	}

	if !i.interactive() {
		return false, entity.ErrScanNotInteractive
	}

	if _, err := fmt.Fprintf(i.out, "\n%s [y/N] ", question); err != nil {
		return false, fmt.Errorf("ask for confirmation: %w", err)
	}

	answer, err := bufio.NewReader(i.in).ReadString('\n')
	if err != nil && answer == "" {
		return false, entity.ErrScanDeclined
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, entity.ErrScanDeclined
	}
}

func (i *Inspection) report(scan control.Scan) error {
	if err := i.line(scan.Inventory.Name + "   " + scan.Inventory.RootPath); err != nil {
		return err
	}

	if err := i.line(""); err != nil {
		return err
	}

	if err := i.tree(scan.Inventory.Repositories); err != nil {
		return err
	}

	return i.summary(scan)
}

func (i *Inspection) tree(repositories []control.Repository) error {
	writer := tabwriter.NewWriter(i.out, 0, 0, 2, ' ', 0)

	for _, row := range treeRows(repositories) {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", row[0], row[1], row[2], row[3]); err != nil {
			return fmt.Errorf("write the scan report: %w", err)
		}
	}

	return writer.Flush()
}

func treeRows(repositories []control.Repository) [][4]string {
	children := map[string][]control.Repository{}
	known := map[string]bool{}

	for _, repository := range repositories {
		known[repository.RelPath] = true
	}

	for _, repository := range repositories {
		parent := repository.Parent
		if !known[parent] {
			parent = ""
		}

		children[parent] = append(children[parent], repository)
	}

	for parent := range children {
		sort.Slice(children[parent], func(a, b int) bool {
			return children[parent][a].RelPath < children[parent][b].RelPath
		})
	}

	rows := make([][4]string, 0, len(repositories))

	var walk func(parent, indent string)

	walk = func(parent, indent string) {
		siblings := children[parent]

		for index, repository := range siblings {
			branch, next := "├─ ", indent+"│  "
			if index == len(siblings)-1 {
				branch, next = "└─ ", indent+"   "
			}

			rows = append(rows, [4]string{
				indent + branch + shorten(repository.RelPath, parent),
				repository.DefaultBranch,
				remoteOf(repository),
				noteOf(repository),
			})

			walk(repository.RelPath, next)
		}
	}

	walk("", "")

	return rows
}

func shorten(relPath, parent string) string {
	if parent == "" {
		return relPath
	}

	return strings.TrimPrefix(strings.TrimPrefix(relPath, parent), "/")
}

func remoteOf(repository control.Repository) string {
	switch {
	case repository.Remote.Host != "" && repository.Remote.PathTail != "":
		return repository.Remote.Host + "/" + repository.Remote.PathTail
	case repository.Remote.PathTail != "":
		return repository.Remote.PathTail
	case repository.Kind == string(entity.RepositoryBare):
		return ""
	default:
		return "no remote"
	}
}

func noteOf(repository control.Repository) string {
	switch entity.RepositoryKind(repository.Kind) {
	case entity.RepositoryWorktree:
		return "linked worktree"
	case entity.RepositoryNested:
		return "nested in " + repository.Parent
	case entity.RepositorySubmodule:
		return "submodule of " + repository.Parent
	case entity.RepositoryBare:
		return "bare repository, ignored"
	default:
		return ""
	}
}

func (i *Inspection) summary(scan control.Scan) error {
	writer := tabwriter.NewWriter(i.out, 0, 0, 3, ' ', 0)

	rows := [][2]string{
		{"shared", listed(scan.Inventory.SharedFiles, "none found")},
		{"tools", toolLine(scan.Inventory.Tools)},
		{"runtimes", listed(scan.Inventory.Runtimes, "process")},
	}

	if scan.Connected {
		rows = append(rows, [2]string{"connected", "yes, as " + scan.CodebaseID})
	}

	if _, err := fmt.Fprintln(i.out); err != nil {
		return fmt.Errorf("write the scan report: %w", err)
	}

	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", row[0], row[1]); err != nil {
			return fmt.Errorf("write the scan report: %w", err)
		}
	}

	for _, change := range driftRows(scan.Drift) {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", change[0], change[1]); err != nil {
			return fmt.Errorf("write the scan report: %w", err)
		}
	}

	for _, warning := range scan.Warnings {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", "warning", warning); err != nil {
			return fmt.Errorf("write the scan report: %w", err)
		}
	}

	return writer.Flush()
}

func driftRows(drift control.Drift) [][2]string {
	rows := make([][2]string, 0, 3)

	for _, change := range [][2]string{
		{"added", strings.Join(drift.Added, ", ")},
		{"removed", strings.Join(drift.Removed, ", ")},
		{"changed", strings.Join(drift.Changed, ", ")},
	} {
		if change[1] != "" {
			rows = append(rows, change)
		}
	}

	return rows
}

func listed(values []string, empty string) string {
	if len(values) == 0 {
		return empty
	}

	return strings.Join(values, ", ")
}

func toolLine(tools []control.Tool) string {
	if len(tools) == 0 {
		return "none installed"
	}

	named := make([]string, 0, len(tools))

	for _, tool := range tools {
		if tool.Version == "" {
			named = append(named, tool.Name)

			continue
		}

		named = append(named, tool.Name+" "+tool.Version)
	}

	return strings.Join(named, ", ")
}

func (i *Inspection) line(text string) error {
	if _, err := fmt.Fprintln(i.out, text); err != nil {
		return fmt.Errorf("write the scan report: %w", err)
	}

	return nil
}

func onATerminal() bool {
	info, err := os.Stdin.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
