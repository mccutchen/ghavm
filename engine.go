package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
	"unicode/utf8"

	renameio "github.com/google/renameio/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/term"

	"github.com/mccutchen/ghavm/internal/slogctx"
	"github.com/mccutchen/ghavm/internal/style"
)

// PinMode instructs [Engine] how to pin action versions.
type PinMode int

// Pin modes.
const (
	ModeCurrent PinMode = iota
	ModeLatest
	ModeCompat
)

func (m PinMode) String() string {
	switch m {
	case ModeCurrent:
		return "current"
	case ModeLatest:
		return "latest"
	case ModeCompat:
		return "latest compatible"
	default:
		panic("invalid PinMode value")
	}
}

// engineOpts customizes engine behavior.
type engineOpts struct {
	// Workers defines the number of worker threads used to resolve actions.
	Workers int
	// Strict enables strict mode, where any action resolution failure aborts
	// the entire process.
	Strict bool
	// Fancy enables "fancy" terminal output via ANSI escape sequences.
	Fancy bool
}

// Engine manages the version upgrade process, from resolving current versions
// to choosing upgrade candidates to applying upgrades.
type Engine struct {
	root     Root
	gh       *GitHubClient
	workers  int
	strict   bool
	style    *style.Style
	phaseLog *PhaseLogger
}

// newEngine creates a new [Engine].
func newEngine(root Root, ghClient *GitHubClient, logOut io.Writer, opts engineOpts) *Engine {
	style := style.New(opts.Fancy)
	phaseLog := &PhaseLogger{
		out:   logOut,
		fancy: opts.Fancy,
		style: style,
	}
	return &Engine{
		root:     root,
		gh:       ghClient,
		workers:  max(opts.Workers, 1),
		strict:   opts.Strict,
		style:    style,
		phaseLog: phaseLog,
	}
}

// List lists each step in each workflow, with the current action version and
// any available upgrades.
func (e *Engine) List(ctx context.Context, dst io.Writer) error {
	if err := e.resolveSteps(ctx, ModeLatest); err != nil {
		return fmt.Errorf("failed to resolve commit refs: %w", err)
	}

	keys := slices.Sorted(maps.Keys(e.root.Workflows))
	for i, key := range keys {
		w := e.root.Workflows[key]
		if len(w.Steps) == 0 {
			continue
		}
		fprintln(dst, "workflow", e.style.Bold(filepath.Base(w.FilePath)))
		for _, s := range w.Steps {
			var (
				current = s.Action.Release
				latest  = s.Action.UpgradeCandidates.Latest
				compat  = s.Action.UpgradeCandidates.LatestCompatible
			)
			fprintf(dst, "  action %s versions:", e.style.Boldf("%s@%s", s.Action.Name, s.Action.Ref))
			fprintln(dst)
			if !current.Exists() {
				fprintln(dst, e.style.Yellow("    (could not resolve action versions, unable to pin or upgrade)"))
				continue
			}
			fprintln(dst, "    current: "+current.String())
			if s.Action.UpgradeCandidates == (UpgradeCandidates{}) {
				fprintln(dst, "    (no upgrade versions found)")
				continue
			} else if latest == current {
				fprintln(dst, e.style.Green("    ✓ already using latest version"))
				continue
			}
			if compat.Exists() {
				msg := compat.String()
				if compat == current {
					msg = e.style.Green("✓ already using latest compat version")
				}
				fprintln(dst, "    compat:  "+msg)
			}
			if latest.Exists() {
				fprintln(dst, "    latest:  "+latest.String())
			}
		}
		if i < len(keys)-1 {
			fprintln(dst)
		}
	}

	return nil
}

// Pin rewrites each workflow's steps from mutable tags/branches to immutable
// commit hashes.
func (e *Engine) Pin(ctx context.Context, mode PinMode) error {
	if err := e.resolveSteps(ctx, mode); err != nil {
		return fmt.Errorf("failed to resolve commit refs: %w", err)
	}
	e.phaseLog.StartPhase("pinning %d action(s) to immutable hashes for their %s versions in %d workflow(s) ...", e.root.StepCount(), mode, e.root.WorkflowCount())
	if err := e.rewriteWorkflows(ctx, rewriteStrategyForMode(mode)); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	e.phaseLog.FinishPhase("done!")
	return nil
}

func (e *Engine) rewriteWorkflows(ctx context.Context, strategy RewriteStrategy) error {
	out := &strings.Builder{}
	for _, w := range e.root.Workflows {
		out.Reset()

		f, err := os.Open(w.FilePath)
		if err != nil {
			return err
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

			// if our strategy did not return a valid release, log and continue
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
				return fmt.Errorf("expected `uses:` declaration on line %d, got %q", lineNum, line)
			}

			// write prefix
			out.WriteString(before + "uses: ")
			// append pinned action version
			fprintf(out, "%s@%s", step.Action.Name, pin.CommitHash)
			// append version hint in comment
			if pin.Version != "" {
				fprintf(out, " # %s", pin.Version)
			} else if step.Action.Ref != pin.CommitHash {
				fprintf(out, " # ref:%s", step.Action.Ref)
			}
			// append correct line ending based on original line
			fprintf(out, matchEOL(line))
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to scan workflow %s: %w", w.FilePath, err)
		}
		slogctx.Debug(
			ctx, "writing pinned file",
			"file", w.FilePath,
		)
		if err := renameio.WriteFile(w.FilePath, []byte(out.String()), 0); err != nil {
			return fmt.Errorf("failed to atomically replace file: %w", err)
		}
	}
	return nil
}

// RewriteStrategy tells the engine's workflow rewriting process how to choose
// an appropriate release to pin.
type RewriteStrategy func(Workflow, Step) Release

func rewriteStrategyForMode(mode PinMode) RewriteStrategy {
	return func(_ Workflow, step Step) Release {
		return chooseUpgrade(step, mode)
	}
}

// chooseUpgrade chooses the best available upgrade from among the step's
// current action version and the two upgrade candidates, based on the mode.
//
// If an expected upgrade candidate was not resolved, default to the current
// release.
func chooseUpgrade(step Step, mode PinMode) Release {
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
		panic("chooseUpgrade: invalid upgrade mode")
	}
}

// resolveSteps walks the set of workflows and attempts to resolve each step's
// current version ref to a concrete commit hash and semver tag, and optionally
// fetches its potential upgrade candidates.
//
// Each step is mutated in-place as it is resolved.
func (e *Engine) resolveSteps(ctx context.Context, mode PinMode) error {
	e.phaseLog.StartPhase("resolving action versions for %d step(s) across %d workflow(s) with %d worker(s) ...", e.root.StepCount(), e.root.WorkflowCount(), e.workers)

	// we can skip the extra work of resolving up to two different upgrade
	// versions if we're only interested in the current versions of our
	// actions (e.g. when running `pin` to pin current deps as-is)
	fetchUpgrades := mode != ModeCurrent

	g, ctx := errgroup.WithContext(ctx)
	var (
		sem          = semaphore.NewWeighted(int64(e.workers))
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
				// if context was canceled, it means another step failed and
				// the whole errgroup will be aborted, so we can let the other
				// failure be reported instead of potentially masking it with
				// an uninformative context cancelation error
				if errors.Is(err, context.Canceled) {
					continue
				}
				err = fmt.Errorf("failed to acquire semaphore: %w", err)
				e.phaseLog.Error(workflow, step, err)
				if e.strict {
					return err
				}
				continue
			}
			g.Go(func() error {
				defer sem.Release(1)
				if err := e.resolveStep(ctx, workflow, step, fetchUpgrades); err != nil {
					e.phaseLog.Error(workflow, step, err)
					if e.strict {
						return err
					}
				}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to resolve actions: %w", err)
	}

	e.phaseLog.FinishPhase("done!")
	e.phaseLog.ShowDiagnostics()
	return nil
}

// resolveStep resolves a single step's current version ref to a concrete
// commit hash and semver tag where possible, and optionally fetches potential
// upgrade candidates.
//
// The given step is mutated in-place.
func (e *Engine) resolveStep(ctx context.Context, workflow Workflow, step *Step, fetchUpgrades bool) error {
	// 1. resolve the version ref (commit, branch, tag, etc) to a specific
	// commit hash
	e.phaseLog.Info(workflow, step, "resolving commit hash for ref %s", step.Action.Ref)
	commit, err := e.gh.GetCommitHashForRef(ctx, step.Action.Name, step.Action.Ref)
	if err != nil {
		return fmt.Errorf("failed to resolve commit hash for ref %s: %w", step.Action.Ref, err)
	}

	// 2a. attempt to find any semver tags pointing to the resolved commit hash.
	e.phaseLog.Info(workflow, step, "resolving semver tags for commit hash %s", commit)
	versions, err := e.gh.GetVersionTagsForCommitHash(ctx, step.Action.Name, commit)
	if err != nil {
		return fmt.Errorf("failed to fetch version tags for resolved commit %s: %w", commit, err)
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
		e.phaseLog.Info(workflow, step, "finding upgrade candidates for version %s", step.Action.Release.Version)
		candidates, err := e.gh.GetUpgradeCandidates(ctx, step.Action.Name, step.Action.Release)
		if err != nil {
			e.phaseLog.Error(workflow, step, fmt.Errorf("failed to get upgrade candidates for version %s: %w", step.Action.Release.Version, err))
		} else if candidates == (UpgradeCandidates{}) {
			e.phaseLog.Warn(workflow, step, fmt.Sprintf("no upgrade candidates found for version %s", step.Action.Release.Version))
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

// Level is a logging/diagnostics level.
type Level slog.Level

func (l Level) String() string {
	return slog.Level(l).String()
}

// Available levels.
const (
	LevelDebug = Level(slog.LevelDebug)
	LevelInfo  = Level(slog.LevelInfo)
	LevelWarn  = Level(slog.LevelWarn)
	LevelError = Level(slog.LevelError)
)

// DiagnosticRecord records something of note that happened during a phase of
// the process.
type DiagnosticRecord struct {
	Level Level
	Step  Step
	Msg   string
}

// PhaseLogger is a minimal, tightly coupled logger providing visibility
// into an [Engine]'s progress.
type PhaseLogger struct {
	mu          sync.Mutex
	out         io.Writer
	diagnostics map[string][]DiagnosticRecord // workflow path -> records

	style *style.Style
	fancy bool

	phaseStarted  atomic.Bool
	inPlaceWrites atomic.Int64
}

// StartPhase logs a header line marking a new phase.
func (pl *PhaseLogger) StartPhase(msg string, args ...any) {
	if pl.phaseStarted.Swap(true) {
		panic("PhaseLogger: current phase must be finished before starting new phase with msg: " + msg)
	}

	// reset diagnostics for new phase
	pl.mu.Lock()
	pl.diagnostics = nil
	pl.mu.Unlock()

	pl.writeln(pl.style.Boldf(msg, args...))
}

// FinishPhase logs a footer line marking the end of a phase.
func (pl *PhaseLogger) FinishPhase(msg string, args ...any) {
	if !pl.phaseStarted.Swap(false) {
		panic("PhaseLogger: no phase to finish with msg: " + msg)
	}
	// if we're finishing a section of overwritten lines, we need to a) reset
	// the write counter to 0 and b) only clear previously overwritten lines
	// if we actually did any previous overwrites
	if pl.inPlaceWrites.Swap(0) > 1 {
		pl.write(cursorUpTwo + carriageReturn + clearToEnd)
	}

	pl.writeln(pl.style.Boldf(msg, args...))
	pl.writeln("")
}

// Info logs an info-level message for a specific [Workflow] and [Step].
func (pl *PhaseLogger) Info(workflow Workflow, step *Step, msg string, args ...any) {
	pl.logPhaseStatus(LevelInfo, workflow, step, msg, args...)
}

// Warn logs an error-level message for a specific [Workflow] and [Step].
func (pl *PhaseLogger) Warn(workflow Workflow, step *Step, msg string, args ...any) {
	msg = fmt.Sprintf(msg, args...)
	pl.logPhaseStatus(LevelWarn, workflow, step, msg)
	pl.addDiagnostic(LevelWarn, workflow, step, msg)
}

// Error logs an error-level message for a specific [Workflow] and [Step].
func (pl *PhaseLogger) Error(workflow Workflow, step *Step, err error) {
	pl.logPhaseStatus(LevelError, workflow, step, err.Error())
	pl.addDiagnostic(LevelError, workflow, step, err.Error())
}

func (pl *PhaseLogger) logPhaseStatus(level Level, workflow Workflow, step *Step, msg string, args ...any) {
	if !pl.phaseStarted.Load() {
		panic("PhaseLogger: phase must be started before updating status: " + msg)
	}
	header := fmt.Sprintf("workflow=%s action=%s", pl.style.Boldf(filepath.Base(workflow.FilePath)), pl.style.Boldf(step.Action.Name))
	msg = fmt.Sprintf(msg, args...)
	switch level {
	case LevelError:
		msg = pl.style.Red(msg)
	case LevelWarn:
		msg = pl.style.Yellow(msg)
	}
	pl.writeInPlace(header, msg)
}

func (pl *PhaseLogger) addDiagnostic(level Level, w Workflow, s *Step, msg string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.diagnostics == nil {
		pl.diagnostics = make(map[string][]DiagnosticRecord)
	}
	key := w.FilePath
	pl.diagnostics[key] = append(pl.diagnostics[key], DiagnosticRecord{
		Level: level,
		Step:  *s,
		Msg:   msg,
	})
}

// ShowDiagnostics shows renders all diagnostics accumulated during a phase.
func (pl *PhaseLogger) ShowDiagnostics() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if len(pl.diagnostics) == 0 {
		return
	}

	maxStepWidth := func(recs []DiagnosticRecord) int {
		width := 0
		for _, rec := range recs {
			width = max(width, len(rec.Step.Action.Name))
		}
		return width
	}

	fprintln(pl.out, pl.style.Boldf("diagnostics"))
	workflowKeys := slices.Sorted(maps.Keys(pl.diagnostics))
	for _, workflow := range workflowKeys {
		recs := pl.diagnostics[workflow]
		msgPrefixTmpl := fmt.Sprintf("%%5s %%-%ds → ", maxStepWidth(recs))
		fprintln(pl.out, " ", pl.style.Boldf(workflow))
		for _, rec := range recs {
			msgPrefix := fmt.Sprintf(msgPrefixTmpl, rec.Level, rec.Step.Action.Name)
			msg := fmt.Sprintf("    %s%s", msgPrefix, rec.Msg)
			switch rec.Level {
			case LevelWarn:
				msg = pl.style.Yellow(msg)
			case LevelError:
				msg = pl.style.Red(msg)
			}
			fprintln(pl.out, msg)
		}
	}

	fprintln(pl.out)
}

const (
	cursorUpTwo    = "\033[2A"
	clearToEnd     = "\033[0J" // clear from cursor to end of screen
	carriageReturn = "\r"
)

func (pl *PhaseLogger) writeln(msg string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	fprintln(pl.out, msg)
}

func (pl *PhaseLogger) write(msg string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	fprint(pl.out, msg)
}

// writeInPlace handles writing status logs, where when "fancy" output is
// enabled we write the header and message on two lines and then overwrite
// those two lines on every subsequent in-place write.
//
// In non-fancy mode, the header and message are written to a single line
// without any overwriting/clearing.
func (pl *PhaseLogger) writeInPlace(header string, msg string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.fancy {
		// only clear previous two lines after the first in-place write
		if pl.inPlaceWrites.Add(1) > 1 {
			fprint(pl.out, cursorUpTwo+carriageReturn+clearToEnd)
		}
		fprintln(pl.out, pl.truncateLine("  "+header))
		fprintln(pl.out, pl.truncateLine("  ↳ "+msg))
	} else {
		fprintln(pl.out, header, "→", msg)
	}
}

func (pl *PhaseLogger) truncateLine(line string) string {
	const minWidth = 40
	if width := getTerminalWidth(pl.out); width > minWidth {
		return truncateToDisplayWidth(line, width)
	}
	return line
}

// getTerminalWidth attempts to get the width of the terminal to which the
// given writer is connected (e.g. if out is os.Stderr). Returns 0 if width
// cannot be determined.
func getTerminalWidth(out io.Writer) int {
	if fder, ok := out.(interface{ Fd() uintptr }); ok {
		if fd := int(fder.Fd()); term.IsTerminal(int(fder.Fd())) {
			if width, _, err := term.GetSize(fd); err == nil {
				return width
			}
		}
	}
	return 0
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// computeDisplayWidth returns the visual width of a string, ignoring ANSI
// escape sequences
func computeDisplayWidth(s string) int {
	cleaned := ansiRegex.ReplaceAllString(s, "")
	return utf8.RuneCountInString(cleaned)
}

// truncateToDisplayWidth truncates a string to fit within the given width,
// preserving ANSI escape sequences and adding ellipsis if needed
func truncateToDisplayWidth(s string, width int) string {
	if computeDisplayWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	var (
		// Split into ANSI sequences and regular text
		parts              = ansiRegex.Split(s, -1)
		sequences          = ansiRegex.FindAllString(s, -1)
		ansiSequencesFound = len(sequences) > 0

		targetWidth  = width - 3 // reserve space for "...""
		result       = ""
		currentWidth = 0
	)

	for i, part := range parts {
		partWidth := utf8.RuneCountInString(part)

		if currentWidth+partWidth <= targetWidth {
			result += part
			currentWidth += partWidth
		} else {
			// Truncate this part to fit exactly
			remaining := targetWidth - currentWidth
			if remaining > 0 {
				runes := []rune(part)
				result += string(runes[:remaining])
			}
			// Always reset if a) we found any ANSI sequences and b) truncated
			// the input, which might have left "dangling" sequences (might be
			// redundant, but seems safe enough and simplifies logic)
			if ansiSequencesFound {
				result += "\033[0m"
			}
			result += "..."
			return result
		}

		// Add the ANSI sequence that follows this part (if any)
		if i < len(sequences) {
			result += sequences[i]
		}
	}

	return result
}
