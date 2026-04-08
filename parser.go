package harnessparser

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
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
	// InterpolateExpressions resolves Harness-style placeholders in values
	// using values and optional fake secrets.
	InterpolateExpressions bool
	// FakeSecrets is a flat key/value map used by interpolation for
	// secrets.getValue expressions.
	FakeSecrets map[string]string
}

// DefaultOptions returns default rendering options
func DefaultOptions() Options {
	return Options{
		StrictMode:             false,
		AddFuncs:               make(map[string]interface{}),
		InterpolateExpressions: false,
		FakeSecrets:            make(map[string]string),
	}
}

// RenderFile renders a template file with values from a values file
func RenderFile(templatePath, valuesPath string, opts ...Options) (string, error) {
	return RenderFileMulti(templatePath, []string{valuesPath}, opts...)
}

// RenderFileMulti renders a template file with values merged from multiple values files.
// Files are applied in order, where later files override earlier files.
func RenderFileMulti(templatePath string, valuesPaths []string, opts ...Options) (string, error) {
	values, err := LoadAndMergeValues(valuesPaths)
	if err != nil {
		return "", fmt.Errorf("failed to load values: %w", err)
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
		if o.InterpolateExpressions {
			options.InterpolateExpressions = true
		}
		for k, v := range o.AddFuncs {
			options.AddFuncs[k] = v
		}
		for k, v := range o.FakeSecrets {
			options.FakeSecrets[k] = v
		}
	}

	if options.InterpolateExpressions {
		InterpolateHarnessExpressions(values, options.FakeSecrets)
	}

	if options.StrictMode {
		if err := CheckExpressions(values); err != nil {
			return "", fmt.Errorf("unresolved expressions: %w", err)
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

var (
	harnessExprPattern       = regexp.MustCompile(`<\+[^>]+>`)
	harnessSimpleExprPattern = regexp.MustCompile(`<\+[A-Za-z0-9_.]+>`)
	harnessSecretExprPattern = regexp.MustCompile(`(?s)<\+secrets\.getValue\(.*?\)>`)
	harnessQuotedStrRegexp   = regexp.MustCompile(`"([^"]+)"`)
)

// InterpolateHarnessExpressions resolves Harness-style placeholders in values.
// It attempts to resolve:
//   - <+path.to.value> from the merged values map
//   - <+secrets.getValue(...)> using fakeSecrets mappings
func InterpolateHarnessExpressions(values map[string]interface{}, fakeSecrets map[string]string) {
	for key, value := range values {
		values[key] = interpolateValue(value, values, fakeSecrets)
	}
}

func interpolateValue(value interface{}, root map[string]interface{}, fakeSecrets map[string]string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			typed[key] = interpolateValue(nested, root, fakeSecrets)
		}
		return typed
	case []interface{}:
		for i, nested := range typed {
			typed[i] = interpolateValue(nested, root, fakeSecrets)
		}
		return typed
	case string:
		return interpolateString(typed, root, fakeSecrets)
	default:
		return value
	}
}

func interpolateString(input string, root map[string]interface{}, fakeSecrets map[string]string) string {
	output := harnessSimpleExprPattern.ReplaceAllStringFunc(input, func(token string) string {
		resolved, ok := resolveExpressionToken(token, root, fakeSecrets)
		if !ok {
			return token
		}
		return resolved
	})

	output = harnessSecretExprPattern.ReplaceAllStringFunc(output, func(token string) string {
		resolved, ok := resolveSecretGetValueToken(token, root, fakeSecrets)
		if !ok {
			return token
		}
		return resolved
	})

	output = harnessSimpleExprPattern.ReplaceAllStringFunc(output, func(token string) string {
		resolved, ok := resolveExpressionToken(token, root, fakeSecrets)
		if !ok {
			return token
		}
		return resolved
	})

	return output
}

func resolveExpressionToken(token string, root map[string]interface{}, fakeSecrets map[string]string) (string, bool) {
	if direct, ok := fakeSecrets[token]; ok {
		return direct, true
	}

	if !strings.HasPrefix(token, "<+") || !strings.HasSuffix(token, ">") {
		return "", false
	}

	expr := strings.TrimSuffix(strings.TrimPrefix(token, "<+"), ">")
	if direct, ok := fakeSecrets[expr]; ok {
		return direct, true
	}

	if strings.HasPrefix(expr, "secrets.getValue(") {
		if secret, ok := resolveSecretExpression(token, expr, fakeSecrets); ok {
			return secret, true
		}
	}

	value, ok := lookupPath(root, expr)
	if !ok {
		return "", false
	}

	return fmt.Sprintf("%v", value), true
}

func resolveSecretGetValueToken(token string, root map[string]interface{}, fakeSecrets map[string]string) (string, bool) {
	if direct, ok := fakeSecrets[token]; ok {
		return direct, true
	}

	if !strings.HasPrefix(token, "<+") || !strings.HasSuffix(token, ">") {
		return "", false
	}
	expr := strings.TrimSuffix(strings.TrimPrefix(token, "<+"), ">")
	if !strings.HasPrefix(expr, "secrets.getValue(") || !strings.HasSuffix(expr, ")") {
		return "", false
	}

	if direct, ok := fakeSecrets[expr]; ok {
		return direct, true
	}

	inner := strings.TrimSuffix(strings.TrimPrefix(expr, "secrets.getValue("), ")")
	segments := splitConcatenationSegments(inner)
	builder := strings.Builder{}

	for _, segment := range segments {
		part := strings.TrimSpace(segment)
		if part == "" {
			continue
		}

		if strings.HasPrefix(part, "\"") && strings.HasSuffix(part, "\"") {
			unquoted, err := strconv.Unquote(part)
			if err != nil {
				return "", false
			}
			builder.WriteString(unquoted)
			continue
		}

		if strings.HasPrefix(part, "<+") && strings.HasSuffix(part, ">") {
			resolved, ok := resolveExpressionToken(part, root, fakeSecrets)
			if !ok {
				return "", false
			}
			builder.WriteString(resolved)
			continue
		}

		if resolved, ok := resolveExpressionToken("<+"+part+">", root, fakeSecrets); ok {
			builder.WriteString(resolved)
			continue
		}

		builder.WriteString(part)
	}

	assembled := builder.String()
	if assembled == "" {
		return "", false
	}

	candidates := []string{assembled, strings.TrimPrefix(assembled, "org.hashicorpvault://")}
	for _, candidate := range candidates {
		if value, ok := fakeSecrets[candidate]; ok {
			return value, true
		}
	}

	trimmed := strings.TrimPrefix(assembled, "org.hashicorpvault://")
	if hashIdx := strings.LastIndex(trimmed, "#"); hashIdx >= 0 && hashIdx+1 < len(trimmed) {
		fragment := trimmed[hashIdx+1:]
		if value, ok := fakeSecrets[fragment]; ok {
			return value, true
		}
	}

	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		if value, ok := fakeSecrets[trimmed[idx+1:]]; ok {
			return value, true
		}
	}

	return "", false
}

func splitConcatenationSegments(input string) []string {
	segments := []string{}
	start := 0
	inQuotes := false

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if ch == '"' && (i == 0 || input[i-1] != '\\') {
			inQuotes = !inQuotes
			continue
		}

		if ch == '+' && !inQuotes {
			segments = append(segments, strings.TrimSpace(input[start:i]))
			start = i + 1
		}
	}

	if start <= len(input) {
		segments = append(segments, strings.TrimSpace(input[start:]))
	}

	return segments
}

func resolveSecretExpression(token, expr string, fakeSecrets map[string]string) (string, bool) {
	if direct, ok := fakeSecrets[token]; ok {
		return direct, true
	}
	if direct, ok := fakeSecrets[expr]; ok {
		return direct, true
	}

	quoted := harnessQuotedStrRegexp.FindAllStringSubmatch(expr, -1)
	for _, match := range quoted {
		if len(match) < 2 {
			continue
		}
		candidate := match[1]
		if value, ok := fakeSecrets[candidate]; ok {
			return value, true
		}

		trimmedPrefix := strings.TrimPrefix(candidate, "org.hashicorpvault://")
		if value, ok := fakeSecrets[trimmedPrefix]; ok {
			return value, true
		}

		if hashIdx := strings.LastIndex(trimmedPrefix, "#"); hashIdx >= 0 && hashIdx+1 < len(trimmedPrefix) {
			fragment := trimmedPrefix[hashIdx+1:]
			if value, ok := fakeSecrets[fragment]; ok {
				return value, true
			}
		}

		if idx := strings.LastIndex(trimmedPrefix, "/"); idx >= 0 && idx+1 < len(trimmedPrefix) {
			leaf := trimmedPrefix[idx+1:]
			if value, ok := fakeSecrets[leaf]; ok {
				return value, true
			}
		}
	}

	return "", false
}

func lookupPath(root map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = root

	for _, part := range parts {
		switch typed := current.(type) {
		case map[string]interface{}:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = next
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}

	return current, true
}

// FlattenStringMap flattens nested YAML map values into dot-path keys for
// interpolation lookups. Non-string values are stringified.
func FlattenStringMap(input map[string]interface{}) map[string]string {
	flat := make(map[string]string)
	flattenInto(flat, "", input)
	return flat
}

func flattenInto(out map[string]string, prefix string, value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			flattenInto(out, nextPrefix, nested)
		}
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprintf("%v", typed)
		}
	}
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

// LoadAndMergeValues loads and merges multiple values YAML files.
// Files are applied in order, where later files override earlier files.
func LoadAndMergeValues(paths []string) (map[string]interface{}, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no values files provided")
	}

	merged := make(map[string]interface{})
	for _, path := range paths {
		values, err := LoadValues(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		mergeMaps(merged, values)
	}

	return merged, nil
}

// LoadValuesBytes loads values from raw YAML bytes
func LoadValuesBytes(data []byte) (map[string]interface{}, error) {
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return values, nil
}

// mergeMaps recursively merges src into dst.
// Nested maps are merged, while non-map values are overwritten by src.
func mergeMaps(dst, src map[string]interface{}) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal
			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			mergeMaps(dstMap, srcMap)
			continue
		}

		dst[key] = srcVal
	}
}

// CheckExpressions checks for unresolved <+...> patterns in values
func CheckExpressions(values map[string]interface{}) error {
	return checkMap(harnessExprPattern, values)
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
