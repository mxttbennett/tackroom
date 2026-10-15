package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// restoreItem is one backed-up path and where it goes back to.
type restoreItem struct {
	Source string
	Target string
	Dir    bool
}

func runRestore(args []string) error {
	return restoreBackup(args, os.Stdout)
}

func restoreBackup(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "Show what would be restored without changing anything")
	positional, err := parseInterleaved(flags, args)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	root := conflictBackupRoot(home)
	backups, err := listBackups(root)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return printBackups(out, root, backups, home)
	}
	if len(positional) > 1 {
		return errors.New("usage: tackroom restore [<backup> | latest] [--dry-run]")
	}
	id := positional[0]
	if id == "latest" {
		if len(backups) == 0 {
			return fmt.Errorf("no backups in %s", root)
		}
		id = backups[0]
	}
	if filepath.Base(id) != id || !stringInSlice(id, backups) {
		return fmt.Errorf("no backup %q in %s; run `tackroom restore` to list them", id, root)
	}
	items, err := collectRestoreItems(filepath.Join(root, id), home)
	if err != nil {
		return err
	}
	safety := newConflictBackupDir(home)
	for n, base := 2, safety; filepath.Base(safety) == id || pathExists(safety); n++ {
		safety = fmt.Sprintf("%s-%d", base, n) // two backups in one second
	}
	restored := 0
	for _, item := range items {
		same, err := restoreTargetMatches(item)
		if err != nil {
			return err
		}
		if same {
			continue
		}
		restored++
		if *dryRun {
			fmt.Fprintf(out, "would restore %s\n", item.Target)
			continue
		}
		if _, err := os.Lstat(item.Target); err == nil {
			// Keep what is there now, so `tackroom restore latest` undoes this.
			if err := moveToBackup(item.Target, backupPathFor(item.Target, safety, home)); err != nil {
				return fmt.Errorf("save current %s: %w", item.Target, err)
			}
		}
		if err := copyRestored(item); err != nil {
			return fmt.Errorf("restore %s: %w", item.Target, err)
		}
		fmt.Fprintf(out, "restored %s\n", item.Target)
	}
	switch {
	case restored == 0:
		fmt.Fprintf(out, "nothing to restore: every path in %s already matches\n", id)
	case *dryRun:
		fmt.Fprintf(out, "%s would change; run without --dry-run to restore\n", pluralPaths(restored))
	default:
		fmt.Fprintf(out, "restored %s from %s; what was there before is in %s (undo with `tackroom restore latest`)\n", pluralPaths(restored), id, safety)
		fmt.Fprintln(out, "note: `tackroom sync` writes managed MCP servers and hooks back, and `sync --replace-conflicts` relinks files; to keep a restored version, drop it from tackroom.yaml or the agent from the config")
	}
	return nil
}

var backupIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z(-\d+)?$`)

// listBackups returns backup ids, newest first.
func listBackups(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() && backupIDPattern.MatchString(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

func printBackups(out io.Writer, root string, ids []string, home string) error {
	if len(ids) == 0 {
		fmt.Fprintf(out, "No backups in %s. tackroom backs up native files before it replaces or removes them.\n", root)
		return nil
	}
	fmt.Fprintf(out, "Backups in %s, newest first:\n", root)
	for _, id := range ids {
		var paths []string
		backupDir := filepath.Join(root, id)
		err := filepath.WalkDir(backupDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if m := numberedBackupPattern.FindStringSubmatch(path); m != nil && pathExists(m[1]) {
				return nil
			}
			rel, err := filepath.Rel(backupDir, path)
			if err != nil {
				return err
			}
			paths = append(paths, displayPath(restoreTargetFor(rel, home), home))
			return nil
		})
		if err != nil {
			return fmt.Errorf("read backup %s: %w", backupDir, err)
		}
		more := ""
		if len(paths) > 3 {
			more = fmt.Sprintf(", +%d more", len(paths)-3)
			paths = paths[:3]
		}
		fmt.Fprintf(out, "  %s  %s%s\n", id, strings.Join(paths, ", "), more)
	}
	fmt.Fprintln(out, "\nRestore one: tackroom restore <backup> --dry-run, then without --dry-run (or: tackroom restore latest)")
	return nil
}

var numberedBackupPattern = regexp.MustCompile(`^(.+)\.\d+$`)

// collectRestoreItems maps a backup directory back onto home. A backed-up
// directory whose target is not a real directory now (a link tackroom made,
// or nothing) is restored as one item, so nothing is written through a link
// into the shared repo. Numbered copies (file.1) are later snapshots from
// the same run; the unnumbered one is the original and wins.
func collectRestoreItems(backupDir string, home string) ([]restoreItem, error) {
	var items []restoreItem
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(backupDir, rel))
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, entry := range entries {
			names[entry.Name()] = true
		}
		for _, entry := range entries {
			if m := numberedBackupPattern.FindStringSubmatch(entry.Name()); m != nil && names[m[1]] {
				continue
			}
			childRel := filepath.Join(rel, entry.Name())
			target := restoreTargetFor(childRel, home)
			source := filepath.Join(backupDir, childRel)
			if entry.IsDir() {
				if info, err := os.Lstat(target); err == nil && info.IsDir() {
					if err := walk(childRel); err != nil {
						return err
					}
					continue
				}
				items = append(items, restoreItem{Source: source, Target: target, Dir: true})
				continue
			}
			items = append(items, restoreItem{Source: source, Target: target})
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, fmt.Errorf("read backup %s: %w", backupDir, err)
	}
	return items, nil
}

// restoreTargetFor reverses backupPathFor.
func restoreTargetFor(rel string, home string) string {
	if rest, ok := strings.CutPrefix(filepath.ToSlash(rel), "abs/"); ok {
		return string(filepath.Separator) + filepath.FromSlash(rest)
	}
	return filepath.Join(home, rel)
}

func restoreTargetMatches(item restoreItem) (bool, error) {
	info, err := os.Lstat(item.Target)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if item.Dir {
		if !info.IsDir() {
			return false, nil
		}
		return externalSkillTreesEqual(item.Source, item.Target)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	return filesEqual(item.Source, item.Target)
}

// copyRestored copies a backup item into place with its original modes.
func copyRestored(item restoreItem) error {
	if err := os.MkdirAll(filepath.Dir(item.Target), 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(item.Source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(item.Source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(item.Target, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(dest, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, dest)
		default:
			return copyBackupFile(path, dest, info.Mode().Perm())
		}
	})
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func pluralPaths(n int) string {
	if n == 1 {
		return "1 path"
	}
	return fmt.Sprintf("%d paths", n)
}
