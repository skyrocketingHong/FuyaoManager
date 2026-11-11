// Package themecontract loads and validates the machine-checkable theme
// contracts under contracts/theme: the theme manifest (theme.json), the
// build-input snapshot metadata (input.json), the release metadata
// (release.json), and theme configuration values checked against a theme's
// own configuration schema.
package themecontract

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Self-identifying contract constants mirrored from the schema files.
const (
	ManifestContract    = "fuyao.theme.manifest/v1"
	BuildInputContract  = "fuyao.theme.build-input/v1"
	ReleaseContract     = "fuyao.theme.release/v1"
	configSchemaDialect = "https://json-schema.org/draft/2020-12/schema"
	configResourceName  = "theme-config.schema.json"
)

// Set holds the compiled theme contract schemas.
type Set struct {
	manifest *jsonschema.Schema
	input    *jsonschema.Schema
	release  *jsonschema.Schema
}

// Load compiles the three contract schemas from dir, which must contain
// manifest.schema.json, build-input.schema.json, and release.schema.json.
func Load(dir string) (*Set, error) {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat() // enforce format: date-time as a real calendar check
	set := &Set{}
	for _, entry := range []struct {
		file   string
		target **jsonschema.Schema
	}{
		{"manifest.schema.json", &set.manifest},
		{"build-input.schema.json", &set.input},
		{"release.schema.json", &set.release},
	} {
		schema, err := compiler.Compile(filepath.Join(dir, entry.file))
		if err != nil {
			return nil, fmt.Errorf("compiling %s: %w", entry.file, err)
		}
		*entry.target = schema
	}
	return set, nil
}

// ValidateManifest validates a theme.json document.
func (s *Set) ValidateManifest(data []byte) error {
	return validateJSON(s.manifest, "theme.json", data)
}

// ValidateInputMetadata validates a build-input input.json document.
func (s *Set) ValidateInputMetadata(data []byte) error {
	return validateJSON(s.input, "input.json", data)
}

// ValidateRelease validates a release.json document.
func (s *Set) ValidateRelease(data []byte) error {
	return validateJSON(s.release, "release.json", data)
}

// ValidateThemeConfigSchema verifies that schemaDoc is a well-formed JSON
// Schema 2020-12 document that compiles. It validates the declared schema
// itself, not any concrete configuration value.
func (s *Set) ValidateThemeConfigSchema(schemaDoc []byte) error {
	_, err := compileThemeConfigSchema(schemaDoc)
	return err
}

// ValidateThemeConfig compiles schemaDoc as a JSON Schema 2020-12 document
// and validates configData against it. The dialect restriction matches the
// manifest configSchema.format constant.
func (s *Set) ValidateThemeConfig(schemaDoc, configData []byte) error {
	schema, err := compileThemeConfigSchema(schemaDoc)
	if err != nil {
		return err
	}
	var configValue any
	if err := json.Unmarshal(configData, &configValue); err != nil {
		return fmt.Errorf("theme config: invalid JSON: %w", err)
	}
	if err := schema.Validate(configValue); err != nil {
		return fmt.Errorf("theme config: %w", err)
	}
	return nil
}

// ValidateEmptyThemeConfig validates a theme configuration for a manifest that
// declares no configSchema: per the contract, only an empty object is valid.
func (s *Set) ValidateEmptyThemeConfig(configData []byte) error {
	var value any
	if err := json.Unmarshal(configData, &value); err != nil {
		return fmt.Errorf("theme config: invalid JSON: %w", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("theme config: top level must be an object")
	}
	if len(object) != 0 {
		return fmt.Errorf("theme config: manifest declares no configSchema, only {} is valid")
	}
	return nil
}

func compileThemeConfigSchema(schemaDoc []byte) (*jsonschema.Schema, error) {
	var schemaValue any
	if err := json.Unmarshal(schemaDoc, &schemaValue); err != nil {
		return nil, fmt.Errorf("theme config schema: invalid JSON: %w", err)
	}
	root, ok := schemaValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("theme config schema: top level must be an object")
	}
	if root["$schema"] != configSchemaDialect {
		return nil, fmt.Errorf("theme config schema: $schema must be %q", configSchemaDialect)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(configResourceName, schemaValue); err != nil {
		return nil, fmt.Errorf("theme config schema: %w", err)
	}
	schema, err := compiler.Compile(configResourceName)
	if err != nil {
		return nil, fmt.Errorf("theme config schema: %w", err)
	}
	return schema, nil
}

func validateJSON(schema *jsonschema.Schema, label string, data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", label, err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// ConfigSchemaFile returns the configuration schema path declared by a
// manifest. Call ValidateManifest first; this helper only extracts the field.
// The second result is false when the manifest declares no configSchema.
func ConfigSchemaFile(manifestData []byte) (string, bool, error) {
	var manifest struct {
		ConfigSchema *struct {
			File string `json:"file"`
		} `json:"configSchema"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", false, fmt.Errorf("theme.json: invalid JSON: %w", err)
	}
	if manifest.ConfigSchema == nil {
		return "", false, nil
	}
	return manifest.ConfigSchema.File, true, nil
}

// CheckPairing verifies that an input.json metadata document was prepared for
// the exact theme a manifest describes: identical theme id and version, and
// an input contract the manifest declares support for.
func CheckPairing(manifestData, inputData []byte) error {
	var manifest struct {
		ID            string `json:"id"`
		Version       string `json:"version"`
		InputProtocol string `json:"inputProtocol"`
	}
	var input struct {
		Contract string `json:"contract"`
		Theme    struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"theme"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("theme.json: invalid JSON: %w", err)
	}
	if err := json.Unmarshal(inputData, &input); err != nil {
		return fmt.Errorf("input.json: invalid JSON: %w", err)
	}
	if input.Contract != manifest.InputProtocol {
		return fmt.Errorf(
			"input.json contract %q does not match manifest inputProtocol %q",
			input.Contract, manifest.InputProtocol,
		)
	}
	if input.Theme.ID != manifest.ID || input.Theme.Version != manifest.Version {
		return fmt.Errorf(
			"input.json theme %s@%s does not match manifest %s@%s",
			input.Theme.ID, input.Theme.Version, manifest.ID, manifest.Version,
		)
	}
	return nil
}

// MissingCapabilities returns the site-required capabilities the manifest
// does not declare, preserving the required order without duplicates.
func MissingCapabilities(manifestData []byte, required []string) ([]string, error) {
	return missingEntries(manifestData, "capabilities", required)
}

// MissingURLConventions returns the site-required URL conventions the
// manifest does not declare, preserving the required order without
// duplicates.
func MissingURLConventions(manifestData []byte, required []string) ([]string, error) {
	return missingEntries(manifestData, "urlConventions", required)
}

func missingEntries(manifestData []byte, field string, required []string) ([]string, error) {
	var manifest struct {
		Outputs *struct {
			URLConventions []string `json:"urlConventions"`
		} `json:"outputs"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("theme.json: invalid JSON: %w", err)
	}
	var declared []string
	switch field {
	case "capabilities":
		declared = manifest.Capabilities
	case "urlConventions":
		if manifest.Outputs == nil {
			return append([]string{}, dedupe(required)...), nil
		}
		declared = manifest.Outputs.URLConventions
	default:
		return nil, fmt.Errorf("unsupported field %q", field)
	}
	supported := make(map[string]bool, len(declared))
	for _, entry := range declared {
		supported[entry] = true
	}
	missing := make([]string, 0)
	for _, entry := range dedupe(required) {
		if !supported[entry] {
			missing = append(missing, entry)
		}
	}
	return missing, nil
}

func dedupe(entries []string) []string {
	result := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		result = append(result, entry)
	}
	return result
}
