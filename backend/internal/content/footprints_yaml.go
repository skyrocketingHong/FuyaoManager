package content

import (
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// The theme accepts a plain name as well as a translated pair. HTTP keeps
// its existing pair projection; unchanged YAML retains the original form.
func (name *FootprintName) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		name.Zh = node.Value
		name.En = node.Value
		return nil
	}
	type translated FootprintName
	var value translated
	if err := node.Decode(&value); err != nil {
		return err
	}
	*name = FootprintName(value)
	return nil
}

// The theme derives spot type from the places container. Legacy generators
// wrote "place"; decoding it never changes that original YAML node on disk.
func (place *FootprintPlace) UnmarshalYAML(node *yaml.Node) error {
	type storedPlace FootprintPlace
	var value storedPlace
	if err := node.Decode(&value); err != nil {
		return err
	}
	if value.Type == "place" || value.Type == "" {
		value.Type = "spot"
	}
	*place = FootprintPlace(value)
	return nil
}

func marshalFootprints(doc *FootprintsDoc, existing []byte) ([]byte, error) {
	if len(existing) == 0 {
		return yaml.Marshal(doc)
	}
	var prior FootprintsDoc
	if err := yaml.Unmarshal(existing, &prior); err != nil {
		return nil, err
	}
	if prior.Cities == nil {
		prior.Cities = []FootprintCity{}
	}
	if reflect.DeepEqual(prior, *doc) {
		return existing, nil
	}
	freshBytes, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var old, fresh yaml.Node
	if err := yaml.Unmarshal(existing, &old); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(freshBytes, &fresh); err != nil {
		return nil, err
	}
	if len(old.Content) != 1 || len(fresh.Content) != 1 {
		return nil, ErrInvalidPath
	}
	mergeFootprintStruct(old.Content[0], fresh.Content[0], reflect.ValueOf(prior), reflect.ValueOf(*doc))
	return yaml.Marshal(&fresh)
}

func yamlField(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}
func setYAMLField(mapping *yaml.Node, key, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key.Value {
			mapping.Content[i], mapping.Content[i+1] = key, value
			return
		}
	}
	mapping.Content = append(mapping.Content, key, value)
}
func mergeFootprintStruct(old, fresh *yaml.Node, before, after reflect.Value) {
	if old == nil || fresh == nil || old.Kind != yaml.MappingNode || fresh.Kind != yaml.MappingNode || before.Kind() != reflect.Struct || after.Kind() != reflect.Struct {
		return
	}
	fields := map[string]int{}
	for i := 0; i < before.NumField(); i++ {
		name := strings.Split(before.Type().Field(i).Tag.Get("yaml"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = i
		}
	}
	fresh.Anchor = old.Anchor
	fresh.HeadComment, fresh.LineComment, fresh.FootComment = old.HeadComment, old.LineComment, old.FootComment
	for i := 0; i+1 < len(old.Content); i += 2 {
		key, value := old.Content[i], old.Content[i+1]
		field, known := fields[key.Value]
		if !known {
			setYAMLField(fresh, key, value)
			continue
		}
		if reflect.DeepEqual(before.Field(field).Interface(), after.Field(field).Interface()) {
			setYAMLField(fresh, key, value)
			continue
		}
		updated := yamlField(fresh, key.Value)
		if updated == nil {
			continue
		}
		if before.Field(field).Kind() == reflect.Struct {
			mergeFootprintStruct(value, updated, before.Field(field), after.Field(field))
		}
		if key.Value == "cities" || key.Value == "places" {
			mergeFootprintSequence(value, updated, before.Field(field), after.Field(field))
		}
		updated.Anchor = value.Anchor
	}
}
func mergeFootprintSequence(old, fresh *yaml.Node, before, after reflect.Value) {
	if old.Kind != yaml.SequenceNode || fresh.Kind != yaml.SequenceNode || before.Kind() != reflect.Slice || after.Kind() != reflect.Slice {
		return
	}
	original := map[string]*yaml.Node{}
	baseline := map[string]reflect.Value{}
	for _, node := range old.Content {
		if id := yamlField(node, "id"); id != nil {
			original[id.Value] = node
		}
	}
	for i := 0; i < before.Len(); i++ {
		entry := before.Index(i)
		baseline[entry.FieldByName("ID").String()] = entry
	}
	for i, node := range fresh.Content {
		if i >= after.Len() {
			return
		}
		entry := after.Index(i)
		id := entry.FieldByName("ID").String()
		if prior, ok := baseline[id]; ok {
			mergeFootprintStruct(original[id], node, prior, entry)
		}
	}
}
