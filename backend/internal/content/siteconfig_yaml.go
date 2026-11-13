package content

import (
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Known fields follow the submitted DTO, including explicit clearing of
// optional values. Theme extensions outside that DTO retain their YAML nodes.
func marshalSiteConfig(doc *SiteConfigDoc, existing []byte) ([]byte, error) {
	freshBytes, err := yaml.Marshal(doc)
	if err != nil || len(existing) == 0 {
		return freshBytes, err
	}
	var old, fresh yaml.Node
	if err := yaml.Unmarshal(existing, &old); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(freshBytes, &fresh); err != nil {
		return nil, err
	}
	if len(old.Content) == 1 && len(fresh.Content) == 1 {
		preserveUnknownYAML(old.Content[0], fresh.Content[0], reflect.TypeOf(SiteConfigDoc{}))
	}
	return yaml.Marshal(&fresh)
}

func preserveUnknownYAML(old, fresh *yaml.Node, schema reflect.Type) {
	if old.Kind != yaml.MappingNode || fresh.Kind != yaml.MappingNode || schema.Kind() != reflect.Struct {
		return
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < schema.NumField(); i++ {
		field := schema.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = field.Type
		}
	}
	for i := 0; i+1 < len(old.Content); i += 2 {
		name := old.Content[i].Value
		field, known := fields[name]
		if !known {
			fresh.Content = append(fresh.Content, old.Content[i], old.Content[i+1])
			continue
		}
		for j := 0; j+1 < len(fresh.Content); j += 2 {
			if fresh.Content[j].Value == name {
				preserveUnknownYAML(old.Content[i+1], fresh.Content[j+1], field)
				break
			}
		}
	}
}
