package booking

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sviatoslav/portbook/internal/state"
)

// CleanupRow describes a check made during this invocation, not a future promise.
type CleanupRow struct {
	Directory string `json:"directory"`
	Service   string `json:"service"`
	Port      int    `json:"port"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
}
type CleanupReport struct {
	Command     string       `json:"command"`
	DryRun      bool         `json:"dry_run"`
	Rows        []CleanupRow `json:"rows"`
	Removed     int          `json:"removed"`
	WouldRemove int          `json:"would_remove"`
	Retained    int          `json:"retained"`
}

// Cleanup never reads the selected pool or pointer. Dry runs use atomic snapshot
// reads without creating a lock or directory. Mutations reread under one lock.
func (s Store) Cleanup(ctx context.Context, dryRun bool) (CleanupReport, error) {
	if s.Config.Coordinator.Dir == "" || !filepath.IsAbs(s.Config.Coordinator.Dir) {
		return CleanupReport{}, errors.New("state directory must be an absolute path")
	}
	report := CleanupReport{Command: "cleanup", DryRun: dryRun, Rows: []CleanupRow{}}
	operation := func() error {
		r, err := s.read()
		if err != nil {
			return err
		}
		evidence := map[string]cleanupOwnerEvidence{}
		owners := make([]string, 0, len(r))
		for owner := range r {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		probe := s.Probe
		if probe == nil {
			probe = Available
		}
		for _, owner := range owners {
			if err := ctx.Err(); err != nil {
				return err
			}
			initial := inspectCleanupOwner(owner)
			evidence[owner] = initial
			reason := initial.reason
			services := make([]string, 0, len(r[owner]))
			for service := range r[owner] {
				services = append(services, service)
			}
			sort.Strings(services)
			for _, service := range services {
				if err := ctx.Err(); err != nil {
					return err
				}
				row := CleanupRow{Directory: owner, Service: service, Port: r[owner][service], Action: "retained", Reason: reason}
				if reason == "owner_missing" {
					free, err := probe(ctx, row.Port)
					switch {
					case err != nil:
						row.Reason = "port_unknown"
					case !free:
						row.Reason = "port_busy_or_unavailable"
					default:
						// Repeat the owner check after the socket probe to narrow filesystem races.
						row.Reason = recheckCleanupOwner(owner, initial)
						if row.Reason == "owner_missing" {
							if dryRun {
								row.Action = "would-remove"
								report.WouldRemove++
							} else {
								row.Action = "removed"
								report.Removed++
							}
						}
					}
				}
				if row.Action == "retained" {
					report.Retained++
				}
				report.Rows = append(report.Rows, row)
			}
		}
		// A later probe can observe filesystem changes after an earlier row was
		// selected. Revalidate every proposed owner before modifying the snapshot.
		finalReasons := map[string]string{}
		for i := range report.Rows {
			row := &report.Rows[i]
			if row.Action == "retained" {
				continue
			}
			reason, checked := finalReasons[row.Directory]
			if !checked {
				reason = recheckCleanupOwner(row.Directory, evidence[row.Directory])
				finalReasons[row.Directory] = reason
			}
			if reason != "owner_missing" {
				if dryRun {
					report.WouldRemove--
				} else {
					report.Removed--
				}
				report.Retained++
				row.Action, row.Reason = "retained", reason
				continue
			}
			if !dryRun {
				delete(r[row.Directory], row.Service)
				if len(r[row.Directory]) == 0 {
					delete(r, row.Directory)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if report.Removed == 0 {
			return nil
		}
		data, err := encode(r)
		if err != nil {
			return err
		}
		return state.AtomicWrite(s.Config.Coordinator.Dir, File, data)
	}
	var err error
	if dryRun {
		err = operation()
	} else {
		err = s.Config.Coordinator.WithLock(ctx, operation)
	}
	if err != nil {
		return CleanupReport{}, err
	}
	return report, nil
}

// Only a missing final component on a verifiably accessible local parent is
// evidence of deletion. Never resolve a newly introduced symlink or infer that
// a missing ancestor (possibly an absent volume) was deliberately deleted.
type cleanupOwnerEvidence struct {
	reason    string
	ancestors []os.FileInfo
}

func cleanupOwner(owner string) string { return inspectCleanupOwner(owner).reason }

func recheckCleanupOwner(owner string, initial cleanupOwnerEvidence) string {
	current := inspectCleanupOwner(owner)
	if current.reason != "owner_missing" {
		return current.reason
	}
	if len(current.ancestors) != len(initial.ancestors) {
		return "owner_uncertain"
	}
	for i, before := range initial.ancestors {
		if !os.SameFile(before, current.ancestors[i]) {
			return "owner_uncertain"
		}
	}
	return current.reason
}

func inspectCleanupOwner(owner string) (evidence cleanupOwnerEvidence) {
	evidence.reason = "owner_uncertain"

	for _, root := range []string{"/Volumes", "/mnt", "/media", "/run/media"} {
		if strings.EqualFold(owner, root) || strings.HasPrefix(strings.ToLower(owner), strings.ToLower(root)+"/") {
			evidence.reason = "owner_uncertain"
			return evidence
		}
	}
	parts := strings.Split(strings.TrimPrefix(owner, string(filepath.Separator)), string(filepath.Separator))
	current := string(filepath.Separator)
	for i, part := range parts {
		parent := current
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && i == len(parts)-1 {
			canonical, resolveErr := filepath.EvalSymlinks(parent)
			if resolveErr != nil || canonical != parent || !localCleanupFilesystem(parent) {
				evidence.reason = "owner_uncertain"
				return evidence
			}
			evidence.reason = "owner_missing"
			return evidence
		}
		if err != nil {
			evidence.reason = "owner_uncertain"
			return evidence
		}
		if info.Mode()&os.ModeSymlink != 0 {
			evidence.reason = "owner_uncertain"
			return evidence
		}
		if !info.IsDir() {
			evidence.reason = "owner_not_directory"
			return evidence
		}
		f, err := os.Open(current)
		if err != nil {
			evidence.reason = "owner_uncertain"
			return evidence
		}
		_, readErr := f.ReadDir(1)
		closeErr := f.Close()
		if (readErr != nil && readErr != io.EOF) || closeErr != nil {
			evidence.reason = "owner_uncertain"
			return evidence
		}
		searched, err := os.Stat(current + string(filepath.Separator) + ".")
		if err != nil || !os.SameFile(info, searched) {
			evidence.reason = "owner_uncertain"
			return evidence
		}
		evidence.ancestors = append(evidence.ancestors, info)
	}
	evidence.reason = "owner_exists"
	return evidence
}
