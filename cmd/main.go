package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	harnessparser "harness-parser"
)

var (
	apiVersionPattern = regexp.MustCompile(`(?m)^\s*apiVersion\s*:`)
	kindPattern       = regexp.MustCompile(`(?m)^\s*kind\s*:`)
	templateLocRegex  = regexp.MustCompile(`template:\s+[^:]+:(\d+)(?::(\d+))?:`)
)

func printHelp(name string) {
	fmt.Fprintf(os.Stderr, "harness-parser - Render Harness pipeline templates with a values file\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  %s <template-or-directory> [values-file]\n", name)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <template-or-directory>  (required) Template file or directory of templates\n")
	fmt.Fprintf(os.Stderr, "  [values-file]  (optional) Path to your values YAML file (default: example-values.yaml)\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s example-template.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s deployment.yaml values.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s ./templates values.yaml\n", name)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Notes:\n")
	fmt.Fprintf(os.Stderr, "  Directory mode is top-level only (not recursive).\n")
	fmt.Fprintf(os.Stderr, "  In directory mode, only .yaml/.yml files containing both apiVersion and kind are rendered.\n")
	fmt.Fprintf(os.Stderr, "  When a custom values file is provided, strict mode is enabled and\n")
	fmt.Fprintf(os.Stderr, "  any unresolved expressions will cause an error.\n")
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

func isK8sTemplateFile(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := string(content)
	return apiVersionPattern.MatchString(text) && kindPattern.MatchString(text), nil
}

func getSourceContext(sourcePath string, line, col int) string {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	sourceLine := lines[line-1]
	if col > 0 {
		caretPad := strings.Repeat(" ", col-1)
		return fmt.Sprintf("\n    %s\n    %s^", sourceLine, caretPad)
	}

	return fmt.Sprintf("\n    %s", sourceLine)
}

func formatRenderFailure(displayName, sourcePath string, err error) string {
	message := err.Error()
	loc := templateLocRegex.FindStringSubmatch(message)
	if len(loc) >= 2 {
		line, lineErr := strconv.Atoi(loc[1])
		if lineErr != nil {
			return fmt.Sprintf("%s:%s: %s", displayName, loc[1], message)
		}

		col := 0
		if len(loc) >= 3 && loc[2] != "" {
			if parsedCol, colErr := strconv.Atoi(loc[2]); colErr == nil {
				col = parsedCol
			}
		}

		sourceContext := getSourceContext(sourcePath, line, col)
		if col > 0 {
			return fmt.Sprintf("%s:%d:%d: %s%s", displayName, line, col, message, sourceContext)
		}
		return fmt.Sprintf("%s:%d: %s%s", displayName, line, message, sourceContext)
	}
	return fmt.Sprintf("%s: %s", displayName, message)
}

func renderSingleTemplate(templatePath, valuesFile string, opts []harnessparser.Options) error {
	output, err := harnessparser.RenderFile(templatePath, valuesFile, opts...)
	if err != nil {
		return fmt.Errorf("%s", formatRenderFailure(templatePath, templatePath, err))
	}

	fmt.Print(output)
	return nil
}

func renderTemplateDirectory(dirPath, valuesFile string, opts []harnessparser.Options) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	files := []string{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if !isYAMLFile(entry.Name()) {
			continue
		}
		files = append(files, entry.Name())
	}

	sort.Strings(files)

	renderedCount := 0
	failures := []string{}

	for _, fileName := range files {
		fullPath := filepath.Join(dirPath, fileName)
		isK8s, err := isK8sTemplateFile(fullPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: failed to read file: %v", fileName, err))
			continue
		}
		if !isK8s {
			continue
		}

		output, err := harnessparser.RenderFile(fullPath, valuesFile, opts...)
		if err != nil {
			failures = append(failures, formatRenderFailure(fileName, fullPath, err))
			continue
		}

		if renderedCount > 0 {
			fmt.Println("---")
		}
		fmt.Println(output)
		renderedCount++
	}

	if renderedCount == 0 && len(failures) == 0 {
		return fmt.Errorf("no Kubernetes template files found in %s", dirPath)
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "\nCompleted with %d failure(s):\n", len(failures))
		for _, failure := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", failure)
		}
		return fmt.Errorf("one or more templates failed to render")
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		printHelp(os.Args[0])
		os.Exit(1)
	}

	templateInput := os.Args[1]
	valuesFile := "example-values.yaml"
	if len(os.Args) > 2 {
		valuesFile = os.Args[2]
	}

	// Determine if we should use strict mode
	isTestValues := valuesFile == "example-values.yaml"

	opts := []harnessparser.Options{}
	if !isTestValues {
		opts = append(opts, harnessparser.Options{StrictMode: true})
	}

	inputInfo, err := os.Stat(templateInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to access input %s: %v\n", templateInput, err)
		os.Exit(1)
	}

	if inputInfo.IsDir() {
		err = renderTemplateDirectory(templateInput, valuesFile, opts)
	} else {
		err = renderSingleTemplate(templateInput, valuesFile, opts)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
