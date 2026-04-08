package harnessparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp file %s: %v", path, err)
	}

	return path
}

func TestLoadAndMergeValues_MergesInOrder(t *testing.T) {
	tempDir := t.TempDir()

	basePath := writeTempFile(t, tempDir, "values.yaml", `
service:
  name: encryption-service
  replicas: 1
  resources:
    limits:
      cpu: 100m
ports:
  - 8080
enabled: true
`)

	envPath := writeTempFile(t, tempDir, "values-stage.yaml", `
service:
  replicas: 2
  resources:
    limits:
      memory: 256Mi
ports:
  - 8443
enabled: false
`)

	merged, err := LoadAndMergeValues([]string{basePath, envPath})
	if err != nil {
		t.Fatalf("LoadAndMergeValues returned error: %v", err)
	}

	service, ok := merged["service"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected service map, got %T", merged["service"])
	}

	if got := service["name"]; got != "encryption-service" {
		t.Fatalf("expected service.name from base, got %v", got)
	}
	if got := service["replicas"]; got != 2 {
		t.Fatalf("expected service.replicas overridden to 2, got %v", got)
	}

	resources, ok := service["resources"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected resources map, got %T", service["resources"])
	}
	limits, ok := resources["limits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected limits map, got %T", resources["limits"])
	}

	if got := limits["cpu"]; got != "100m" {
		t.Fatalf("expected cpu from base to remain, got %v", got)
	}
	if got := limits["memory"]; got != "256Mi" {
		t.Fatalf("expected memory from env override, got %v", got)
	}

	ports, ok := merged["ports"].([]interface{})
	if !ok {
		t.Fatalf("expected ports slice, got %T", merged["ports"])
	}
	if len(ports) != 1 || ports[0] != 8443 {
		t.Fatalf("expected ports replaced by env value, got %v", ports)
	}

	if got := merged["enabled"]; got != false {
		t.Fatalf("expected enabled overridden to false, got %v", got)
	}
}

func TestLoadAndMergeValues_RequiresFiles(t *testing.T) {
	_, err := LoadAndMergeValues(nil)
	if err == nil {
		t.Fatal("expected error when no values files are provided")
	}
}

func TestRenderFileMulti_UsesLayeredValues(t *testing.T) {
	tempDir := t.TempDir()

	templatePath := writeTempFile(t, tempDir, "template.yaml", `
name: {{ .Values.service.name }}
replicas: {{ .Values.service.replicas }}
region: {{ .Values.service.region }}
`)

	basePath := writeTempFile(t, tempDir, "values.yaml", `
service:
  name: encryption-service
  replicas: 1
  region: us
`)

	envPath := writeTempFile(t, tempDir, "values-stage.yaml", `
service:
  replicas: 2
`)

	envRegionPath := writeTempFile(t, tempDir, "values-stage-az1.yaml", `
service:
  region: az1-us
`)

	out, err := RenderFileMulti(templatePath, []string{basePath, envPath, envRegionPath})
	if err != nil {
		t.Fatalf("RenderFileMulti returned error: %v", err)
	}

	if !strings.Contains(out, "name: encryption-service") {
		t.Fatalf("expected output to contain base name, got:\n%s", out)
	}
	if !strings.Contains(out, "replicas: 2") {
		t.Fatalf("expected output to contain env replicas override, got:\n%s", out)
	}
	if !strings.Contains(out, "region: az1-us") {
		t.Fatalf("expected output to contain env-region override, got:\n%s", out)
	}
}

func TestInterpolateHarnessExpressions_ResolvesValuesAndSecrets(t *testing.T) {
	values := map[string]interface{}{
		"service": map[string]interface{}{
			"name": "enc-svc",
		},
		"pipeline": map[string]interface{}{
			"variables": map[string]interface{}{
				"tag":         "1.2.3",
				"secretsPath": "team/path",
			},
		},
		"meta": map[string]interface{}{
			"appName":      "<+service.name>",
			"image":        "repo/app:<+pipeline.variables.tag>",
			"secret":       "<+secrets.getValue(\"org.hashicorpvault://team/path/db-password\")>",
			"secretNested": "<+secrets.getValue(\"org.hashicorpvault://\" + <+pipeline.variables.secretsPath> + \"/db-password\")>",
		},
	}

	fakeSecrets := map[string]string{
		"team/path/db-password": "fake-db-pass",
	}

	InterpolateHarnessExpressions(values, fakeSecrets)

	meta := values["meta"].(map[string]interface{})
	if got := meta["appName"]; got != "enc-svc" {
		t.Fatalf("expected appName to resolve from values, got %v", got)
	}
	if got := meta["image"]; got != "repo/app:1.2.3" {
		t.Fatalf("expected image tag interpolation, got %v", got)
	}
	if got := meta["secret"]; got != "fake-db-pass" {
		t.Fatalf("expected secret interpolation from fake secrets, got %v", got)
	}
	if got := meta["secretNested"]; got != "fake-db-pass" {
		t.Fatalf("expected nested secret interpolation from fake secrets, got %v", got)
	}
}

func TestFlattenStringMap_FlattensNestedMaps(t *testing.T) {
	input := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "value",
			"c": 2,
		},
		"d": true,
	}

	flat := FlattenStringMap(input)

	if got := flat["a.b"]; got != "value" {
		t.Fatalf("expected a.b to be value, got %v", got)
	}
	if got := flat["a.c"]; got != "2" {
		t.Fatalf("expected a.c to be stringified 2, got %v", got)
	}
	if got := flat["d"]; got != "true" {
		t.Fatalf("expected d to be stringified true, got %v", got)
	}
}
