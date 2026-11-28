package chiklang

import (
	"fmt"
	"strings"
)

// ToYAML renders a parsed .chik file as the YAML document that expresses the
// same declaration. The compiler already validates YAML, so this is how a
// .chik project reaches the same validators instead of growing a second copy
// of every rule.
//
// The mapping is total for the beta grammar. A construct the beta refuses is
// already reported by Parse, so a file that reaches here has no CHK0601.
//
// An entry the mapping does not know is an error, not a silent omission: a
// dropped field would compile into a project that means something else than
// the file says.
func ToYAML(file *File) ([]byte, error) {
	var out strings.Builder
	types := declaredTypes(file)
	for _, block := range file.Root.Children {
		var err error
		switch block.Kind {
		case NodeProjectBlock:
			err = renderProject(file, &out, block)
		case NodeSchemaBlock:
			err = renderSchema(file, &out, block)
		case NodeTypesBlock:
			// YAML describes the record at each use, without a type registry.
		case NodeServicesBlock:
			err = renderServices(file, &out, block, types)
		case NodeClientsBlock:
			err = renderClients(file, &out, block)
		case NodeAuthBlock:
			err = renderAuth(file, &out, block)
		default:
			return nil, fmt.Errorf("%s: no YAML form", block.Kind)
		}
		if err != nil {
			return nil, err
		}
	}
	return []byte(out.String()), nil
}

// unknownEntry names a field the .chik form accepts as syntax but has no YAML
// mapping for, which is a declaration error rather than something to drop. The
// entry's own range is reported so a reader can find the line.
func unknownEntry(file *File, entry *Node) error {
	return &Error{
		Diagnostics: []Diagnostic{{
			Code: CodeConvertUnsupported, Severity: "error", File: file.Path,
			Path: entry.Path, Message: fmt.Sprintf("%s has no YAML form", entry.Name),
			// The name alone is underlined; the value can be several lines and
			// does not need to be read to fix this.
			Range: Range{
				Start: entry.Range.Start,
				End:   Pos{Line: entry.Range.Start.Line, Column: entry.Range.Start.Column + len([]rune(entry.Name))},
			},
		}},
		Sources: map[string]string{file.Path: file.Source},
	}
}

func indent(level int) string { return strings.Repeat("  ", level) }

// scalarOf returns a node's literal text, quoted when it is a string.
func scalarOf(node *Node) (string, bool) {
	if node == nil {
		return "", false
	}
	if len(node.Tokens) == 1 {
		token := node.Tokens[0]
		switch token.Kind {
		case String:
			return fmt.Sprintf("%q", token.Text), true
		case Number:
			return token.Text, true
		}
	}
	if node.Name != "" {
		return node.Name, true
	}
	return "", false
}

// renderProject writes the project file's YAML form. The YAML places name
// under project, storage, vault, and capabilities at the top level, and the
// server and clients under generation, so each part is collected on its own and
// joined in that order. Writing keys as they appear would nest a key under
// whichever section the previous key opened.
func renderProject(file *File, out *strings.Builder, block *Node) error {
	var project, storage, server, clients, vault, realtime, capabilities strings.Builder
	for _, entry := range block.Children {
		switch entry.Name {
		case "name":
			if value, ok := scalarOf(child(entry, 0)); ok {
				fmt.Fprintf(&project, "  name: %s\n", value)
			}
		case "server":
			fmt.Fprintf(&server, "    language: %s\n", quoteIfString(child(entry, 0)))
		case "secrets":
			fmt.Fprintf(&server, "    secrets: %s\n", listOf(child(entry, 0)))
		case "clients":
			fmt.Fprintf(&clients, "  clients: %s\n", listOf(child(entry, 0)))
		case "storage":
			storage.WriteString("storage:\n")
			for _, pair := range child(entry, 0).Children {
				fmt.Fprintf(&storage, "  %s: %s\n", pair.Name, quoteIfString(child(pair, 0)))
			}
		case "vault":
			if value, ok := scalarOf(child(entry, 0)); ok {
				fmt.Fprintf(&vault, "vault: %s\n", value)
			}
		case "realtime":
			if value, ok := scalarOf(child(entry, 0)); ok {
				fmt.Fprintf(&realtime, "realtime: %s\n", value)
			}
		case "capabilities":
			// The capability map is name to reviewed identifier. The retired
			// extensions list had no author handle, so it cannot express this.
			// Capabilities sit beside project and generation in the YAML form.
			value := child(entry, 0)
			if value == nil || value.Kind != NodeTypeRecord {
				// A list has no name, and the name is what a capability needs.
				return &Error{
					Diagnostics: []Diagnostic{{
						Code: CodeConvertUnsupported, Severity: "error", File: file.Path,
						Path:    entry.Path,
						Message: "capabilities must be a name-keyed map, such as { control_plane: \"platform/control-plane\" }",
						Range: Range{
							Start: entry.Range.Start,
							End:   Pos{Line: entry.Range.Start.Line, Column: entry.Range.Start.Column + len([]rune(entry.Name))},
						},
					}},
					Sources: map[string]string{file.Path: file.Source},
				}
			}
			capabilities.WriteString("capabilities:\n")
			seen := map[string]bool{}
			for _, pair := range value.Children {
				// The YAML decoder reports a duplicate at a line the .chik file
				// does not have, so it is refused here with the real position.
				if seen[pair.Name] {
					return &Error{
						Diagnostics: []Diagnostic{{
							Code: CodeDuplicateName, Severity: "error", File: file.Path,
							Path:    strings.TrimPrefix(entry.Path+"."+pair.Name, "."),
							Message: fmt.Sprintf("capability %q is declared twice", pair.Name),
							Range: Range{
								Start: pair.Range.Start,
								End:   Pos{Line: pair.Range.Start.Line, Column: pair.Range.Start.Column + len([]rune(pair.Name))},
							},
						}},
						Sources: map[string]string{file.Path: file.Source},
					}
				}
				if !lowerSnake.MatchString(pair.Name) {
					return &Error{
						Diagnostics: []Diagnostic{{
							Code: CodeInvalidName, Severity: "error", File: file.Path,
							Path:    strings.TrimPrefix(entry.Path+"."+pair.Name, "."),
							Message: fmt.Sprintf("capability name %q must use lower_snake_case", pair.Name),
							Range: Range{
								Start: pair.Range.Start,
								End:   Pos{Line: pair.Range.Start.Line, Column: pair.Range.Start.Column + len([]rune(pair.Name))},
							},
						}},
						Sources: map[string]string{file.Path: file.Source},
					}
				}
				seen[pair.Name] = true
				fmt.Fprintf(&capabilities, "  %s: %s\n", pair.Name, quoteIfString(child(pair, 0)))
			}
		default:
			return unknownEntry(file, entry)
		}
	}
	out.WriteString("project:\n")
	out.WriteString(project.String())
	out.WriteString(storage.String())
	if server.Len() != 0 || clients.Len() != 0 {
		out.WriteString("generation:\n")
		if server.Len() != 0 {
			out.WriteString("  server:\n")
			out.WriteString(server.String())
		}
		out.WriteString(clients.String())
	}
	out.WriteString(vault.String())
	out.WriteString(realtime.String())
	out.WriteString(capabilities.String())
	return nil
}

func child(node *Node, index int) *Node {
	if node == nil || index >= len(node.Children) {
		return nil
	}
	return node.Children[index]
}

func quoteIfString(node *Node) string {
	value, ok := scalarOf(node)
	if !ok {
		return ""
	}
	return value
}

func listOf(node *Node) string {
	if node == nil {
		return "[]"
	}
	var values []string
	for _, item := range node.Children {
		values = append(values, quoteIfString(item))
	}
	return "[" + strings.Join(values, ", ") + "]"
}

func renderSchema(file *File, out *strings.Builder, block *Node) error {
	out.WriteString("database:\n")
	for _, entry := range block.Children {
		switch entry.Name {
		case "profile":
			fmt.Fprintf(out, "  profile: %s\n", quoteIfString(child(entry, 0)))
		case "tables":
			// A nested block's children are the tables themselves.
			out.WriteString("  tables:\n")
			for _, table := range entry.Children {
				fmt.Fprintf(out, "    %s:\n", table.Name)
				for _, group := range table.Children {
					if group.Name == "columns" {
						out.WriteString("      columns:\n")
						for _, column := range group.Children {
							fmt.Fprintf(out, "        %s: %s\n", column.Name, columnYAML(child(column, 0)))
						}
						continue
					}
					// primary_key is one list, but unique, foreign_keys,
					// indexes, and checks are name maps whose entries hold
					// several fields. Writing them as a block keeps the
					// structure the compiler reads; a single flow mapping
					// would drop it.
					if value := child(group, 0); value != nil && value.Kind == NodeTypeList {
						fmt.Fprintf(out, "      %s: %s\n", group.Name, listOf(value))
						continue
					}
					blockYAML(out, 3, group)
				}
			}
		default:
			return unknownEntry(file, entry)
		}
	}
	return nil
}

// blockYAML renders a nested block entry as an indented mapping. A field with a
// single value is written inline; a field whose children are themselves
// entries is another level, so a check predicate or an index definition keeps
// its shape.
func blockYAML(out *strings.Builder, level int, entry *Node) {
	fmt.Fprintf(out, "%s%s:\n", indent(level), entry.Name)
	for _, field := range entry.Children {
		value := child(field, 0)
		if value == nil {
			continue
		}
		switch value.Kind {
		case NodeTypeRef, NodeTypeList, NodeTypeRecord, NodeTypeNullable:
			fmt.Fprintf(out, "%s%s: %s\n", indent(level+1), field.Name, inlineOf(field))
		default:
			blockYAML(out, level+1, field)
		}
	}
}

// inlineOf renders an entry's value as one YAML flow mapping, which is the
// shape the compiler reads for a column, key, index, or check.
func inlineOf(node *Node) string {
	if node == nil {
		return "{}"
	}
	// An entry holds its value as the first child.
	value := node
	if node.Kind == NodeEntry && len(node.Children) == 1 {
		value = node.Children[0]
	}
	switch value.Kind {
	case NodeTypeList:
		return listOf(value)
	case NodeTypeRecord:
		var parts []string
		for _, field := range value.Children {
			parts = append(parts, fmt.Sprintf("%s: %s", field.Name, inlineOf(field)))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	if literal, ok := scalarOf(value); ok {
		return literal
	}
	return "{}"
}

// declaredTypes maps each named record in the file to its declaration.
func declaredTypes(file *File) map[string]*Node {
	types := map[string]*Node{}
	for _, block := range file.Root.Children {
		if block.Kind != NodeTypesBlock {
			continue
		}
		for _, decl := range block.Children {
			types[decl.Name] = decl
		}
	}
	return types
}

func renderServices(file *File, out *strings.Builder, block *Node, types map[string]*Node) error {
	out.WriteString("services:\n")
	for _, service := range block.Children {
		fmt.Fprintf(out, "  %s:\n    operations:\n", service.Name)
		for _, operation := range service.Children {
			fmt.Fprintf(out, "      %s:\n", operation.Name)
			// Every parameter belongs to one input mapping. Opening it per
			// parameter would repeat the key, which YAML rejects.
			inputOpen := false
			for _, part := range operation.Children {
				switch part.Kind {
				case NodeParam:
					if !inputOpen {
						out.WriteString("        input:\n")
						inputOpen = true
					}
					value, err := typeYAML(file, fullType(part), types)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "          %s: %s\n", part.Name, withOptional(value, part.Optional))
				case NodeOperationPrice:
					return &Error{Diagnostics: []Diagnostic{operationPriceUnavailable(part, file.Path)},
						Sources: map[string]string{file.Path: file.Source}}
				case NodeResult:
					if len(part.Children) == 0 {
						continue
					}
					out.WriteString("        output:\n")
					for _, field := range part.Children {
						value, err := typeYAML(file, fullType(field), types)
						if err != nil {
							return err
						}
						fmt.Fprintf(out, "          %s: %s\n", field.Name, withOptional(value, field.Optional))
					}
				}
			}
		}
	}
	return nil
}

// fullType returns a field or parameter's complete type expression. The
// parser appends each modifier after the element it wraps, so the last node
// is the outermost one and reading the first alone would drop every modifier.
func fullType(node *Node) *Node {
	if len(node.Children) == 0 {
		return nil
	}
	return node.Children[len(node.Children)-1]
}

// innerType returns the type a modifier wraps: the last of its children for
// the same reason fullType takes the last of a field's.
func innerType(node *Node) *Node {
	if len(node.Children) == 0 {
		return nil
	}
	return node.Children[len(node.Children)-1]
}

// typeError reports a construct the YAML form cannot express, at the position
// the author wrote it. Refusing here keeps a declaration that compiles as
// something narrower (a list that silently becomes its element type, for
// example) out of the project.
func typeError(file *File, node *Node, message string) error {
	return &Error{
		Diagnostics: []Diagnostic{{
			Code: CodeConvertUnsupported, Severity: "error", File: file.Path,
			Path: node.Path, Message: message, Range: node.Range,
		}},
		Sources: map[string]string{file.Path: file.Source},
	}
}

// withOptional adds the YAML optional flag to a field's metadata. Only a field
// metadata map takes it: a record's fields belong to the record.
func withOptional(metadata string, optional bool) string {
	if !optional || !strings.HasPrefix(metadata, "{type: ") {
		return metadata
	}
	return strings.TrimSuffix(metadata, "}") + ", optional: true}"
}

// typeYAML renders a type expression as YAML field metadata.
func typeYAML(file *File, node *Node, types map[string]*Node) (string, error) {
	if node == nil {
		return "{type: string}", nil
	}
	switch node.Kind {
	case NodeTypeNullable:
		inner, err := typeYAML(file, innerType(node), types)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(inner, "}") + ", nullable: true}", nil
	case NodeTypeList:
		return listYAML(file, node, types)
	case NodeTypeRecord:
		return recordYAML(file, node, types, "object", "fields")
	}
	if declared := types[node.Name]; declared != nil {
		return typeYAML(file, innerType(declared), types)
	}
	return fmt.Sprintf("{type: %s}", node.Name), nil
}

// listYAML renders an object list. The item fields come from the element
// record, expanded in place when the element names a declared type.
func listYAML(file *File, node *Node, types map[string]*Node) (string, error) {
	element := innerType(node)
	if element != nil && element.Kind == NodeTypeRef {
		if declared := types[element.Name]; declared != nil {
			element = innerType(declared)
		}
	}
	if element == nil || element.Kind != NodeTypeRecord {
		name := "this element"
		if element != nil && element.Name != "" {
			name = element.Name
		}
		return "", typeError(file, node, fmt.Sprintf("a list of %s has no YAML form; the YAML surface declares object lists only, so give the element fields", name))
	}
	return recordYAML(file, element, types, "list", "items")
}

func recordYAML(file *File, record *Node, types map[string]*Node, kind, key string) (string, error) {
	parts := make([]string, 0, len(record.Children))
	for _, field := range record.Children {
		value, err := typeYAML(file, fullType(field), types)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s: %s", field.Name, withOptional(value, field.Optional)))
	}
	return fmt.Sprintf("{type: %s, %s: {%s}}", kind, key, strings.Join(parts, ", ")), nil
}

func renderClients(file *File, out *strings.Builder, block *Node) error {
	out.WriteString("clients:\n")
	for _, entry := range block.Children {
		fmt.Fprintf(out, "  %s:\n", entry.Name)
		for _, field := range child(entry, 0).Children {
			fmt.Fprintf(out, "    %s: %s\n", field.Name, quoteIfString(child(field, 0)))
		}
	}
	return nil
}

func renderAuth(file *File, out *strings.Builder, block *Node) error {
	out.WriteString("auth:\n")
	for _, entry := range block.Children {
		// The auth keys the compiler knows. Anything else is refused here with a
		// position rather than handed to YAML, whose error names no line in the
		// .chik file.
		switch entry.Name {
		case "methods", "origins", "signup", "browser_session", "session", "email", "rate_limits":
		default:
			return unknownEntry(file, entry)
		}
		value := child(entry, 0)
		if value != nil && value.Kind == NodeTypeList {
			fmt.Fprintf(out, "  %s: %s\n", entry.Name, listOf(value))
			continue
		}
		if value != nil && value.Kind == NodeTypeRecord && len(value.Children) != 0 {
			fmt.Fprintf(out, "  %s:\n", entry.Name)
			for _, field := range value.Children {
				fmt.Fprintf(out, "    %s: %s\n", field.Name, inlineOf(field))
			}
			continue
		}
		if scalar, ok := scalarOf(value); ok {
			fmt.Fprintf(out, "  %s: %s\n", entry.Name, scalar)
			continue
		}
		return unknownEntry(file, entry)
	}
	return nil
}

// columnYAML renders one column's metadata as a flow mapping.
func columnYAML(node *Node) string {
	if node == nil {
		return "{}"
	}
	var parts []string
	for _, field := range node.Children {
		parts = append(parts, fmt.Sprintf("%s: %s", field.Name, inlineOf(field)))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
