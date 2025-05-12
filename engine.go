package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fatih/color"
	renameio "github.com/google/renameio/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/mccutchen/ghavm/internal/slogctx"
)

type UpgradeMode int

const (
	ModeCurrent UpgradeMode = iota
	ModeLatest
	ModeCompat
)

func (m UpgradeMode) String() string {
	switch m {
	case ModeCurrent:
		return "current"
	case ModeLatest:
		return "latest"
	case ModeCompat:
		return "latest compatible"
	default:
		panic("invalid UpgradeMode value")
	}
}

var (
	bold  = color.New(color.Bold).SprintFunc()
	boldf = color.New(color.Bold).SprintfFunc()
)

// Engine manages the version upgrade process, from resolving current versions
// to choosing upgrade candidates to applying upgrades.
type Engine struct {
	root        Root
	gh          *GitHubClient
	parallelism int
	log         *Logger
}

// newEngine creates a new [Engine].
func newEngine(root Root, ghClient *GitHubClient, parallelism int, logOut io.Writer) *Engine {
	log := &Logger{out: logOut}
	log.precomputeColumnWidths(root)
	return &Engine{
		gh:          ghClient,
		root:        root,
		parallelism: parallelism,
		log:         log,
	}
}

// List lists each step in each workfow, with the current action version and
// any available upgrades.
func (e *Engine) List(ctx context.Context, dst io.Writer) error {
	if err := e.resolveSteps(ctx, ModeLatest); err != nil {
		return fmt.Errorf("engine: failed to resolve commit refs: %w", err)
	}

	keys := slices.Sorted(maps.Keys(e.root.Workflows))
	for _, key := range keys {
		w := e.root.Workflows[key]
		if len(w.Steps) == 0 {
			continue
		}
		fmt.Fprintln(dst, "")
		fmt.Fprintf(dst, "in workflow %s", bold(filepath.Base(w.FilePath)))
		fmt.Fprintln(dst, "")
		for _, s := range w.Steps {
			var (
				current = s.Action.Release
				latest  = s.Action.UpgradeCandidates.Latest
				compat  = s.Action.UpgradeCandidates.LatestCompatible
			)
			fmt.Fprintf(dst, "  action %s has available versions:", boldf("%s@%s", s.Action.Name, s.Action.Ref))
			fmt.Fprintln(dst, "")
			if !current.Exists() {
				fmt.Fprintln(dst, "  (could not resolve action versions, unable to pin or upgrade)")
				continue
			}
			fmt.Fprintln(dst, "    current: "+current.String())
			if s.Action.UpgradeCandidates == (UpgradeCandidates{}) {
				fmt.Fprintln(dst, "    (no upgrade versions found)")
				continue
			} else if latest == current {
				fmt.Fprintln(dst, "    ✓ already using latest version")
				continue
			}
			if compat.Exists() {
				msg := compat.String()
				if compat == current {
					msg = "✓ already using latest compat version"
				}
				fmt.Fprintln(dst, "    compat:  "+msg)
			}
			if latest.Exists() {
				fmt.Fprintln(dst, "    latest:  "+latest.String())
			}
		}
	}

	return nil
}

// Pin rewrites each workflow's steps from mutable tags/branches to immutable
// commit hashes.
func (e *Engine) Pin(ctx context.Context, mode UpgradeMode) error {
	if err := e.resolveSteps(ctx, mode); err != nil {
		return fmt.Errorf("engine: failed to resolve commit refs: %w", err)
	}
	e.log.StartSection("pinning %d action(s) to immutable hashes for their %s versions in %d workflow(s) ...", e.root.StepCount(), mode, e.root.WorkflowCount())
	defer e.log.FinishSection("done!")
	if err := e.rewriteWorkflows(ctx, rewriteStrategyForMode(mode)); err != nil {
		return fmt.Errorf("engine: upgrade failed: %w", err)
	}
	return nil
}

func (e *Engine) rewriteWorkflows(ctx context.Context, strategy RewriteStrategy) error {
	out := &strings.Builder{}
	for _, w := range e.root.Workflows {
		out.Reset()

		f, err := os.Open(w.FilePath)
		if err != nil {
			return fmt.Errorf("engine: %w", err)
		}

		steps := stepsByLine(w.Steps)
		scanner := bufio.NewScanner(f)
		scanner.Split(scanLinesWithEndings)
		for lineNum := 0; scanner.Scan(); lineNum++ {
			line := scanner.Text()
			step, found := steps[lineNum]
			if !found {
				out.WriteString(line)
				continue
			}

			// figure out which version we're pinning, if any
			pin := strategy(w, step)

			// if our strategy did not return a valid release, log and contine
			//
			// TODO: better diagnostics
			if !pin.Exists() {
				slogctx.Debug(
					ctx, "skipping unresolved action",
					"action", fmt.Sprintf("%s@%s", step.Action.Name, step.Action.Ref),
				)
				out.WriteString(line)
				continue
			}

			before, _, found := strings.Cut(line, "uses:")
			if !found {
				return fmt.Errorf("engine: expected `uses:` declaration on line %d, got %q", lineNum, line)
			}

			// write prefix
			out.WriteString(before + "uses: ")
			// append pinned action version
			fmt.Fprintf(out, "%s@%s", step.Action.Name, pin.CommitHash)
			// append version hint in comment
			if pin.Version != "" {
				fmt.Fprintf(out, " # %s", pin.Version)
			} else if step.Action.Ref != pin.CommitHash {
				fmt.Fprintf(out, " # ref:%s", step.Action.Ref)
			}
			// append correct line ending based on original line
			fmt.Fprint(out, matchEOL(line))
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("engine: %w", err)
		}
		slogctx.Debug(
			ctx, "writing pinned file",
			"file", w.FilePath,
		)
		if err := renameio.WriteFile(w.FilePath, []byte(out.String()), 0); err != nil {
			return fmt.Errorf("engine: failed to atomically replace file: %w", err)
		}
	}
	return nil
}

// RewriteStrategy tells the engine's workflow rewriting process how to choose
// an appropriate release to pin.
type RewriteStrategy func(Workflow, Step) Release

func rewriteStrategyForMode(mode UpgradeMode) RewriteStrategy {
	return func(w Workflow, step Step) Release {
		return chooseUpgrade(step, mode)
	}
}

// chooseUpgrade chooses the best available upgrade from among the step's
// current action version and the two upgrade candidates, based on the mode.
//
// If an expected upgrade candidate was not resolved, default to the current
// release.
func chooseUpgrade(step Step, mode UpgradeMode) Release {
	current := step.Action.Release
	candidates := step.Action.UpgradeCandidates
	switch mode {
	case ModeCompat:
		if candidates.LatestCompatible.Exists() {
			return candidates.LatestCompatible
		}
		return current
	case ModeLatest:
		if candidates.Latest.Exists() {
			return candidates.Latest
		}
		return current
	case ModeCurrent:
		return current
	default:
		panic("engine: invalid upgrade mode")
	}
}

// resolveSteps walks the set of workflows and attempts to resolve each step's
// current version ref to a concrete commit hash and semver tag, and optinally
// fetches its potential upgrade candidates.
//
// Each step is mutated in-place as it is resolved.
func (e *Engine) resolveSteps(ctx context.Context, mode UpgradeMode) error {
	e.log.StartSection("resolving action versions for %d steps across %d workflows ...", e.root.StepCount(), e.root.WorkflowCount())
	defer e.log.FinishSection("done!")

	// we can skip the extra work of resolving up to two different upgrade
	// versions if we're only interested in the current versions of our
	// actions (e.g. when running `pin` to pin current deps as-is)
	fetchUpgrades := mode != ModeCurrent

	g, ctx := errgroup.WithContext(ctx)
	var (
		sem          = semaphore.NewWeighted(int64(e.parallelism))
		workflowKeys = slices.Sorted(maps.Keys(e.root.Workflows))
	)
	for _, key := range workflowKeys {
		workflow := e.root.Workflows[key]
		for j := range workflow.Steps {
			// take pointer to step via manually indexing into our tree so
			// modifications will persist beyond this function call
			step := &workflow.Steps[j]

			// don't schedule more than N concurrent tasks
			if err := sem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("engine: failed to aquire semaphore: %w", err)
			}
			g.Go(func() error {
				defer sem.Release(1)
				if err := e.resolveStep(ctx, workflow, step, fetchUpgrades); err != nil {
					e.log.StepError(workflow, step, err)
					return err
				}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("engine: failed to resolve actions: %w", err)
	}
	return nil
}

// resolveStep resolves a signle step's current version ref to a concrete
// commit hash and semver tag where possible, and optionally fetches potential
// upgrade candidates.
//
// The given step is mutated in-place.
func (e *Engine) resolveStep(ctx context.Context, workflow Workflow, step *Step, fetchUpgrades bool) error {
	// 1. resolve the version ref (commit, branch, tag, etc) to a specific
	// commit hash
	e.log.StepInfo(workflow, step, "resolving commit hash for ref %s", step.Action.Ref)
	commit, err := e.gh.GetCommitHashForRef(ctx, step.Action.Name, step.Action.Ref)
	if err != nil {
		return fmt.Errorf("failed to resolve commit hash for ref %s@%s: %w", step.Action.Name, step.Action.Ref, err)
	}

	// 2a. attempt to find any semver tags pointing to the resolved commit hash.
	e.log.StepInfo(workflow, step, "resolving semver tags for commit hash %s", commit)
	versions, err := e.gh.GetVersionTagsForCommitHash(ctx, step.Action.Name, commit)
	if err != nil {
		return fmt.Errorf("failed to fetch version tags for resolved commit %s@%s: %w", step.Action.Name, commit, err)
	}

	// 2b. it's conceivable that some commits will point to multiple
	// version tags (e.g. v4, v4.1, v4.1.2), but the versions are returned in
	// sorted order so we can just take the first as the best version.
	//
	// it's also entirely possible that a commit will NOT correspond
	// to a version tag; in that case, the versions slice will be
	// empty, which will leave version as an empty string.
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}

	// at this point, we have resolved the action's current ref to at least
	// a concrete commit hash and maybe a specific semver version.
	step.Action.Release = Release{
		CommitHash: commit,
		Version:    version,
	}
	slogctx.Debug(
		ctx, "engine: resolved current version ref to semver tags",
		"action", step.Action.Name,
		"ref", step.Action.Ref,
		"commit", commit,
		"versions", versions,
		"release", step.Action.Release,
	)

	// 3. (optionally) fetch potential upgrade candidate versions for the
	// current release.
	if fetchUpgrades {
		e.log.StepInfo(workflow, step, "finding upgrade candidates for version %s", step.Action.Release.Version)
		candidates, err := e.gh.GetUpgradeCandidates(ctx, step.Action.Name, step.Action.Release)
		if err != nil {
			return fmt.Errorf("engine: failed to get upgrade candidates for version %s@%s: %w", step.Action.Name, step.Action.Release.Version, err)
		}
		step.Action.UpgradeCandidates = candidates
	}
	return nil
}

// stepsByLine groups a slice of [Step]s into a map by line number
func stepsByLine(steps []Step) map[int]Step {
	m := make(map[int]Step, len(steps))
	for _, s := range steps {
		m[s.LineNumber] = s
	}
	return m
}

// scanLinesWithEndings is a custom [bufio.SplitFunc] that works like the
// default [bufio.ScanLines] but includes the line endings (either \n or \r\n)
// with each scanned line.
//
// Note that checking for the presence of \n covers both line endings, so no
// special handling for \r\n is required.
//
// See upstream ScanLines implementation here:
// https://github.com/golang/go/blob/go1.24.3/src/bufio/scan.go#L349-L369
func scanLinesWithEndings(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF {
		if len(data) == 0 {
			return 0, nil, nil
		}
		return len(data), data, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[0 : i+1], nil
	}
	return 0, nil, nil
}

var eolPattern = regexp.MustCompile(`\r?\n$`)

func matchEOL(line string) string {
	return eolPattern.FindString(line)
}

const (
	cursorUpOne    = "\033[1A"
	clearLine      = "\033[2K"
	carriageReturn = "\r"
	hideCursor     = "\033[?25l"
	showCursor     = "\033[?25h"
)

// Logger is a minimal, tightly coupled logger providing visibility into an
// [Engine]'s progress.
type Logger struct {
	mu  sync.Mutex
	out io.Writer

	stepWritten   atomic.Bool
	workflowWidth int
	stepWidth     int
}

// StartSection logs a header line marking a new phase.
func (l *Logger) StartSection(msg string, args ...any) {
	l.writeln("")
	l.writeln(boldf(msg, args...))
}

// StepInfo logs an info-level message for a specific [Workflow] and [Step].
func (l *Logger) StepInfo(workflow Workflow, step *Step, msg string, args ...any) {
	l.stepLog(slog.LevelInfo, workflow, step, msg, args...)
}

// StepError logs an error-level message for a specific [Workflow] and [Step].
func (l *Logger) StepError(workflow Workflow, step *Step, err error) {
	l.stepLog(slog.LevelError, workflow, step, err.Error())
}

func (l *Logger) FinishSection(msg string, args ...any) {
	l.writeln(boldf(msg, args...))
	l.stepWritten.Store(false)
}

func (l *Logger) stepLog(level slog.Level, workflow Workflow, step *Step, msg string, args ...any) {
	prefixTmpl := fmt.Sprintf("file=%%-%ds action=%%-%ds → ", l.workflowWidth, l.stepWidth)
	prefix := fmt.Sprintf(prefixTmpl, filepath.Base(workflow.FilePath), step.Action.Name)
	msg = fmt.Sprintf(prefix+msg, args...)
	l.writeln(msg)
	l.stepWritten.Store(true)
}

func (l *Logger) writeln(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stepWritten.Load() && !color.NoColor {
		msg = cursorUpOne + clearLine + carriageReturn + msg
	}
	fmt.Fprintln(l.out, msg)
}

func (l *Logger) precomputeColumnWidths(root Root) {
	for _, workflow := range root.Workflows {
		l.workflowWidth = max(l.workflowWidth, len(filepath.Base(workflow.FilePath)))
		for _, step := range workflow.Steps {
			l.stepWidth = max(l.stepWidth, len(step.Action.Name))
		}
	}
}
