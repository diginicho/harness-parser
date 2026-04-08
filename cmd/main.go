package main

import (
	"flag"
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
	fmt.Fprintf(os.Stderr, "harness-parser - Render Harness pipeline templates with layered values files\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  %s [flags] <template-or-directory> [values-file ...]\n", name)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <template-or-directory>  (required) Template file or directory of templates\n")
	fmt.Fprintf(os.Stderr, "  [values-file ...]  (optional) One or more values YAML files, or a single values directory (default: example-values.yaml)\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  -env <name>          Environment selector for values directory mode (e.g. prod, stage)\n")
	fmt.Fprintf(os.Stderr, "  -interpolate-secrets   Resolve Harness placeholders using values and fake secrets\n")
	fmt.Fprintf(os.Stderr, "  -secrets-file <path>   Path to fake secrets YAML file (optional)\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s example-template.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s deployment.yaml values.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s ./templates ./values\n", name)
	fmt.Fprintf(os.Stderr, "  %s ./templates ./values -env prod\n", name)
	fmt.Fprintf(os.Stderr, "  %s deployment.yaml values-base.yaml values-stage.yaml values-az1.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s ./templates values.yaml\n", name)
	fmt.Fprintf(os.Stderr, "  %s -interpolate-secrets ./templates values.yaml values-stage.yaml values-stage-az1.yaml\n", name)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Notes:\n")
	fmt.Fprintf(os.Stderr, "  Directory mode is top-level only (not recursive).\n")
	fmt.Fprintf(os.Stderr, "  In directory mode, only .yaml/.yml files containing both apiVersion and kind are rendered.\n")
	fmt.Fprintf(os.Stderr, "  Multiple values files are merged in argument order (later files override earlier files).\n")
	fmt.Fprintf(os.Stderr, "  If a single values directory is provided, values files are auto-discovered:\n")
	fmt.Fprintf(os.Stderr, "    values.yaml first, then values-*.yaml in lexical order (top-level only).\n")
	fmt.Fprintf(os.Stderr, "  With -env <name> and values directory mode, selected files are:\n")
	fmt.Fprintf(os.Stderr, "    values.yaml + values-<env>.yaml + values-<env>-*.yaml (top-level only).\n")
	fmt.Fprintf(os.Stderr, "  With -interpolate-secrets and no -secrets-file, adjacent secrets files are auto-discovered:\n")
	fmt.Fprintf(os.Stderr, "    <values-basename>.secrets.yaml and secrets.yaml\n")
	fmt.Fprintf(os.Stderr, "  When a custom values file is provided, strict mode is enabled and\n")
	fmt.Fprintf(os.Stderr, "  any unresolved expressions will cause an error.\n")
}

func normalizeArgsForFlags(raw []string, valuedFlags map[string]bool) ([]string, error) {
	flags := []string{}
	positionals := []string{}

	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		if isValued, ok := valuedFlags[arg]; ok {
			if isValued {
				if i+1 >= len(raw) {
					return nil, fmt.Errorf("flag %s requires a value", arg)
				}
				flags = append(flags, arg, raw[i+1])
				i++
				continue
			}
			flags = append(flags, arg)
			continue
		}

		positionals = append(positionals, arg)
	}

	return append(flags, positionals...), nil
}

func discoverAdjacentSecretsFiles(valuesFiles []string) []string {
	result := []string{}
	seen := make(map[string]struct{})

	for _, valuesFile := range valuesFiles {
		dir := filepath.Dir(valuesFile)
		base := strings.TrimSuffix(filepath.Base(valuesFile), filepath.Ext(valuesFile))
		candidates := []string{
			filepath.Join(dir, base+".secrets.yaml"),
			filepath.Join(dir, "secrets.yaml"),
		}

		for _, candidate := range candidates {
			if _, already := seen[candidate]; already {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				result = append(result, candidate)
				seen[candidate] = struct{}{}
			}
		}
	}

	return result
}

func loadFakeSecretsMap(secretsFiles []string) (map[string]string, error) {
	fakeSecrets := make(map[string]string)

	for _, secretsFile := range secretsFiles {
		values, err := harnessparser.LoadValues(secretsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load secrets file %s: %w", secretsFile, err)
		}
		flat := harnessparser.FlattenStringMap(values)
		for key, value := range flat {
			fakeSecrets[key] = value
		}
	}

	return fakeSecrets, nil
}

func discoverValuesFilesInDir(valuesDir, env string) ([]string, error) {
	entries, err := os.ReadDir(valuesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read values directory %s: %w", valuesDir, err)
	}

	baseFile := ""
	exactEnvFile := ""
	envVariantFiles := []string{}
	genericFiles := []string{}
	envPrefix := ""
	envExactName := ""
	if env != "" {
		envPrefix = "values-" + env + "-"
		envExactName = "values-" + env
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		name := entry.Name()
		if !isYAMLFile(name) {
			continue
		}
		if name == "secrets.yaml" || strings.HasSuffix(name, ".secrets.yaml") {
			continue
		}
		if !strings.HasPrefix(name, "values") {
			continue
		}

		fullPath := filepath.Join(valuesDir, name)
		baseName := strings.TrimSuffix(name, filepath.Ext(name))

		if name == "values.yaml" || name == "values.yml" {
			baseFile = fullPath
			continue
		}

		if env != "" {
			if baseName == envExactName {
				exactEnvFile = fullPath
				continue
			}
			if strings.HasPrefix(baseName, envPrefix) {
				envVariantFiles = append(envVariantFiles, fullPath)
			}
			continue
		}

		genericFiles = append(genericFiles, fullPath)
	}

	if baseFile == "" {
		return nil, fmt.Errorf("values directory %s is missing values.yaml", valuesDir)
	}

	sort.Slice(envVariantFiles, func(i, j int) bool {
		return filepath.Base(envVariantFiles[i]) < filepath.Base(envVariantFiles[j])
	})
	sort.Slice(genericFiles, func(i, j int) bool {
		return filepath.Base(genericFiles[i]) < filepath.Base(genericFiles[j])
	})

	files := []string{baseFile}
	if env != "" {
		if exactEnvFile != "" {
			files = append(files, exactEnvFile)
		}
		files = append(files, envVariantFiles...)
		if len(files) == 1 {
			return nil, fmt.Errorf("no env-specific values files found for env %q in %s", env, valuesDir)
		}
		return files, nil
	}

	files = append(files, genericFiles...)
	return files, nil
}

func resolveValuesInputs(valuesArgs []string, env string) ([]string, error) {
	if len(valuesArgs) == 0 {
		return []string{"example-values.yaml"}, nil
	}

	if len(valuesArgs) == 1 {
		info, err := os.Stat(valuesArgs[0])
		if err != nil {
			return nil, fmt.Errorf("failed to access values input %s: %w", valuesArgs[0], err)
		}
		if info.IsDir() {
			return discoverValuesFilesInDir(valuesArgs[0], env)
		}
	}

	if env != "" {
		return nil, fmt.Errorf("-env is only supported when values input is a single directory")
	}

	for _, valueInput := range valuesArgs {
		info, err := os.Stat(valueInput)
		if err != nil {
			return nil, fmt.Errorf("failed to access values input %s: %w", valueInput, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("values directory input is only supported when passed as a single values argument")
		}
	}

	return valuesArgs, nil
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

func renderSingleTemplate(templatePath string, valuesFiles []string, opts []harnessparser.Options) error {
	output, err := harnessparser.RenderFileMulti(templatePath, valuesFiles, opts...)
	if err != nil {
		return fmt.Errorf("%s", formatRenderFailure(templatePath, templatePath, err))
	}

	fmt.Print(output)
	return nil
}

func renderTemplateDirectory(dirPath string, valuesFiles []string, opts []harnessparser.Options) error {
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

		output, err := harnessparser.RenderFileMulti(fullPath, valuesFiles, opts...)
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
	normalizedArgs, err := normalizeArgsForFlags(os.Args[1:], map[string]bool{
		"-interpolate-secrets": false,
		"-secrets-file":        true,
		"-env":                 true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	env := fs.String("env", "", "Environment selector for values directory mode (e.g. prod, stage)")
	interpolateSecrets := fs.Bool("interpolate-secrets", false, "Resolve Harness placeholders using values and fake secrets")
	secretsFile := fs.String("secrets-file", "", "Path to fake secrets YAML file")

	if err := fs.Parse(normalizedArgs); err != nil {
		printHelp(os.Args[0])
		os.Exit(1)
	}

	args := fs.Args()
	if len(args) < 1 {
		printHelp(os.Args[0])
		os.Exit(1)
	}

	templateInput := args[0]
	valuesFiles, err := resolveValuesInputs(args[1:], strings.TrimSpace(*env))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Determine if we should use strict mode
	isDefaultValues := len(valuesFiles) == 1 && valuesFiles[0] == "example-values.yaml"

	opts := []harnessparser.Options{}
	option := harnessparser.Options{}
	if !isDefaultValues {
		option.StrictMode = true
	}

	if *interpolateSecrets {
		secretsFiles := []string{}
		if *secretsFile != "" {
			secretsFiles = append(secretsFiles, *secretsFile)
		} else {
			secretsFiles = discoverAdjacentSecretsFiles(valuesFiles)
		}

		if len(secretsFiles) == 0 {
			fmt.Fprintf(os.Stderr, "Error: interpolation requested but no secrets files found. Provide -secrets-file or add adjacent secrets files.\n")
			os.Exit(1)
		}

		fakeSecrets, err := loadFakeSecretsMap(secretsFiles)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		option.InterpolateExpressions = true
		option.FakeSecrets = fakeSecrets
	}
	opts = append(opts, option)

	inputInfo, err := os.Stat(templateInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to access input %s: %v\n", templateInput, err)
		os.Exit(1)
	}

	if inputInfo.IsDir() {
		err = renderTemplateDirectory(templateInput, valuesFiles, opts)
	} else {
		err = renderSingleTemplate(templateInput, valuesFiles, opts)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
