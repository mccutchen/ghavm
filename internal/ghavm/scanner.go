package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FindWorkflows finds any workflow yaml files in the standard location under
// the given repo root dir.
func FindWorkflows(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return findWorkflowsInRepo("."), nil
	}

	var files []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			if gitInfo, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				if gitInfo.IsDir() {
					files = append(files, findWorkflowsInRepo(path)...)
				}
			}
			files = append(files, findWorkflowsInDir(path)...)
		} else {
			files = append(files, path)
		}
	}
	return files, nil
}

func findWorkflowsInRepo(rootDir string) []string {
	workflowDir := filepath.Join(rootDir, ".github", "workflows")
	return findWorkflowsInDir(workflowDir)
}

func findWorkflowsInDir(dir string) []string {
	workflowGlob := filepath.Join(dir, "*.y*ml") // match *.yml and *.yaml
	files, err := filepath.Glob(workflowGlob)
	if err != nil {
		panic(err) // only possible with illegal glob pattern
	}
	return files
}

// scanOpts configures the workflow scanner.
type scanOpts struct {
	Selects  []string
	Excludes []string
}

// ScanWorkflows walks the given files and parses them into a tree of
// workflows and steps.
func ScanWorkflows(filePaths []string, opts scanOpts) (Root, error) {
	root := Root{
		Workflows: make(map[string]Workflow, len(filePaths)),
	}
	for _, f := range filePaths {
		workflow, err := scanFile(f, opts)
		if err != nil {
			return Root{}, err
		}
		root.Workflows[f] = workflow
	}
	return root, nil
}

func scanFile(filePath string, opts scanOpts) (Workflow, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return Workflow{}, fmt.Errorf("scanner: failed to open file %s: %w", filePath, err)
	}

	var steps []Step
	scanner := bufio.NewScanner(f)
	for lineNum := 0; scanner.Scan(); lineNum++ {
		line := scanner.Text()
		action := maybeParseAction(line)
		if action == (Action{}) {
			continue
		}
		// Excludes take precedence, so we select first then exclude
		if len(opts.Selects) > 0 && !matchesAnyPattern(action.Name, opts.Selects) {
			continue
		}
		if len(opts.Excludes) > 0 && matchesAnyPattern(action.Name, opts.Excludes) {
			continue
		}
		steps = append(steps, Step{
			LineNumber: lineNum,
			Action:     action,
		})
	}
	if err := scanner.Err(); err != nil {
		return Workflow{}, fmt.Errorf("error scanning file %s: %w", filePath, err)
	}
	return Workflow{
		FilePath: filePath,
		Steps:    steps,
	}, nil
}

// matchesPattern checks if a string matches a pattern with optional trailing wildcard.
// Supports patterns like "actions/*" but not complex patterns like "*/setup".
func matchesPattern(s, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(s, prefix)
	}
	return s == pattern
}

// matchesAnyPattern checks if a string matches any pattern in the given slice.
func matchesAnyPattern(s string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(s, pattern) {
			return true
		}
	}
	return false
}

// validatePattern checks if a pattern is supported. Only trailing wildcards are allowed.
func validatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern not allowed")
	}

	// Check for unsupported wildcard positions
	if strings.Contains(pattern, "*") {
		if !strings.HasSuffix(pattern, "*") {
			return fmt.Errorf("wildcards are only supported at the end of patterns, got: %q", pattern)
		}
		// Ensure there's only one wildcard and it's at the end
		if strings.Count(pattern, "*") > 1 {
			return fmt.Errorf("multiple wildcards not supported, got: %q", pattern)
		}
	}

	return nil
}

// usesPattern is a regex that attempts to match "uses:" declarations in a
// workflow yaml file.
//
// Yes, this regex is now hairy enough to definitely be in "now you have 2
// problems" territory.
//
// Explore matches:
// https://regex101.com/r/0gKnNw/2
var usesPattern = regexp.MustCompile(`^\s*-?\s*uses:\s*([\w\-]+/[\w\-]+(?:/[\w\-\.]+)*(?:\.ya?ml)?)@([\w\-\./]+)(?:\s*#.*)?$`)

func maybeParseAction(line string) Action {
	matches := usesPattern.FindStringSubmatch(line)
	if matches == nil {
		return Action{}
	}
	return Action{
		Name: matches[1],
		Ref:  matches[2],
	}
}
