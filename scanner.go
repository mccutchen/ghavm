package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// ScanWorkflows walks the given files and parses them into a tree of
// workflows and steps.
func ScanWorkflows(filePaths []string, targetActions []string) (Root, error) {
	root := Root{
		Workflows: make(map[string]Workflow, len(filePaths)),
	}
	for _, f := range filePaths {
		workflow, err := scanFile(f, targetActions)
		if err != nil {
			return Root{}, err
		}
		root.Workflows[f] = workflow
	}
	return root, nil
}

func scanFile(filePath string, targetActions []string) (Workflow, error) {
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
		if len(targetActions) > 0 && !slices.Contains(targetActions, action.Name) {
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

var usesPattern = regexp.MustCompile(`^\s*-?\s*uses:\s*([\w\-]+/[\w\-]+)@([\w\-\./]+)(?:\s*#.*)?$`)

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
