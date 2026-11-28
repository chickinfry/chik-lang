package chiklang

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// refusal reports a declaration the converter cannot express without loss.
// Conversion is not allowed to drop a key: a lost default, a lost one_of
// group, or an unknown field would change the declaration silently, and the
// converted file would look complete.
type refusal struct {
	path    string
	message string
}

// checkConvertible walks the document and collects every construct with no
// .chik form. It runs before rendering so a refusal never leaves a partial
// result.
func checkConvertible(role string, node *yaml.Node) []refusal {
	var context apiMapping
	if role == "api" {
		context = apiDocument
	}
	var c convertCheck
	c.walk(node, "", context)
	return c.refusals
}

type convertCheck struct {
	refusals []refusal
}

type apiMapping int

const (
	otherMapping apiMapping = iota
	apiDocument
	apiServiceNames
	apiService
	apiOperationNames
	apiOperation
	apiFieldNames
	apiField
)

// child identifies maps of customer names separately from their declarations.
func (context apiMapping) child(name string) apiMapping {
	switch context {
	case apiDocument:
		if name == "services" {
			return apiServiceNames
		}
	case apiServiceNames:
		return apiService
	case apiService:
		if name == "operations" {
			return apiOperationNames
		}
	case apiOperationNames:
		return apiOperation
	case apiOperation:
		if name == "input" || name == "output" {
			return apiFieldNames
		}
	case apiFieldNames:
		return apiField
	case apiField:
		if name == "fields" || name == "items" {
			return apiFieldNames
		}
	}
	return otherMapping
}

func (c *convertCheck) refuse(path, message string) {
	c.refusals = append(c.refusals, refusal{path: path, message: message})
}

// walk rejects aliases in any role and unsupported settings in API declarations.
func (c *convertCheck) walk(node *yaml.Node, path string, context apiMapping) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		for _, entry := range pairs(node) {
			child := path + "." + entry.name
			if path == "" {
				child = entry.name
			}
			if context == apiOperation && entry.name == "price" {
				c.refuse(child, operationPriceUnavailableMessage)
			}
			if context == apiOperation || context == apiField {
				c.checkKey(entry.name, child, entry.node)
			}
			c.walk(entry.node, child, context.child(entry.name))
		}
	case yaml.SequenceNode:
		for index, item := range node.Content {
			c.walk(item, fmt.Sprintf("%s[%d]", path, index), otherMapping)
		}
	case yaml.AliasNode:
		c.refuse(path, "an alias has no .chik form")
	}
}

// checkKey refuses the keys the beta grammar cannot express.
func (c *convertCheck) checkKey(name, path string, value *yaml.Node) {
	switch name {
	case "one_of":
		c.refuse(path, "one_of groups have no .chik form yet; keep this declaration in YAML")
	case "default":
		c.refuse(path, "an input default has no .chik form yet; keep this declaration in YAML")
	case "offline", "method", "public":
		if value.Kind != yaml.ScalarNode || value.Value != "false" || name == "public" {
			c.refuse(path, name+" has no .chik form yet; keep this declaration in YAML")
		}
	}
}
