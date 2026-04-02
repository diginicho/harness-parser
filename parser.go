package harnessparser

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"gopkg.in/yaml.v3"
)

// Options for template rendering
type Options struct {
	// StrictMode errors on unresolved <+...> expressions in values
	StrictMode bool
	// AddFuncs allows adding custom template functions
	AddFuncs map[string]interface{}
}

// DefaultOptions returns default rendering options
func DefaultOptions() Options {
	return Options{
		StrictMode: false,
		AddFuncs:   make(map[string]interface{}),
	}
}

// RenderFile renders a template file with values from a values file
func RenderFile(templatePath, valuesPath string, opts ...Options) (string, error) {
	values, err := LoadValues(valuesPath)
	if err != nil {
		return "", fmt.Errorf("failed to load values: %w", err)
	}

	options := DefaultOptions()
	for _, o := range opts {
		if o.StrictMode {
			options.StrictMode = true
		}
		for k, v := range o.AddFuncs {
			options.AddFuncs[k] = v
		}
	}

	if options.StrictMode {
		if err := CheckExpressions(values); err != nil {
			return "", fmt.Errorf("unresolved expressions: %w", err)
		}
	}

	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template: %w", err)
	}

	return Render(string(templateContent), values, opts...)
}

// Render renders template content with values map
func Render(templateContent string, values map[string]interface{}, opts ...Options) (string, error) {
	options := DefaultOptions()
	for _, o := range opts {
		if o.StrictMode {
			options.StrictMode = true
		}
		for k, v := range o.AddFuncs {
			options.AddFuncs[k] = v
		}
	}

	// Wrap values under .Values to match template expectations
	data := map[string]interface{}{
		"Values": values,
	}

	// Create template with Sprig functions (matching Harness)
	funcs := sprig.TxtFuncMap()

	// Add toYaml/fromYaml which Sprig doesn't have (but Harness/Helm does)
	funcs["toYaml"] = toYaml
	funcs["fromYaml"] = fromYaml

	// Add custom functions
	funcs["joinArray"] = joinArray

	// Add user-provided custom functions
	for name, fn := range options.AddFuncs {
		funcs[name] = fn
	}

	// Parse template
	t, err := template.New("template").Funcs(funcs).Parse(templateContent)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	// Render using ExecuteTemplate
	var buf strings.Builder
	err = t.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("render error: %w", err)
	}

	return buf.String(), nil
}

// LoadValues loads and parses a values YAML file
func LoadValues(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return values, nil
}

// LoadValuesBytes loads values from raw YAML bytes
func LoadValuesBytes(data []byte) (map[string]interface{}, error) {
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return values, nil
}

// CheckExpressions checks for unresolved <+...> patterns in values
func CheckExpressions(values map[string]interface{}) error {
	re := regexp.MustCompile(`<\+[^>]+>`)
	return checkMap(re, values)
}

func checkMap(re *regexp.Regexp, m map[string]interface{}) error {
	for _, v := range m {
		if err := checkValue(re, v); err != nil {
			return err
		}
	}
	return nil
}

func checkSlice(re *regexp.Regexp, s []interface{}) error {
	for _, v := range s {
		if err := checkValue(re, v); err != nil {
			return err
		}
	}
	return nil
}

func checkValue(re *regexp.Regexp, v interface{}) error {
	switch val := v.(type) {
	case string:
		if re.MatchString(val) {
			matches := re.FindAllString(val, -1)
			return fmt.Errorf("unresolved Harness expression(s): %v", matches)
		}
	case map[string]interface{}:
		return checkMap(re, val)
	case []interface{}:
		return checkSlice(re, val)
	}
	return nil
}

// toYaml converts a value to YAML string
func toYaml(v interface{}) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// fromYaml parses a YAML string to a value
func fromYaml(s string) (interface{}, error) {
	var v interface{}
	err := yaml.Unmarshal([]byte(s), &v)
	return v, err
}

// joinArray joins array elements with a separator
func joinArray(separator string, items interface{}) (string, error) {
	switch v := items.(type) {
	case []interface{}:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(parts, separator), nil
	case []string:
		return strings.Join(v, separator), nil
	default:
		return "", fmt.Errorf("joinArray expects array, got %T", items)
	}
}
