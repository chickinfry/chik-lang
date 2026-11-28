package chiklang

import (
	"fmt"
	"path/filepath"
	"strings"
)

// parser builds a concrete syntax tree from tokens and trivia. It recovers from
// errors by emitting an Error node and continuing, so one pass reports every
// problem rather than only the first.
type parser struct {
	src    string
	tokens []Token
	trivia []Trivia
	pos    int
	diags  []Diagnostic
}

// Parse reads one declaration file. It always returns a tree, even when the
// input is broken.
func Parse(path, source string) *File {
	tokens, trivia, diags := newLexer(source).run()
	p := &parser{src: source, tokens: tokens, trivia: trivia, diags: diags}
	role := filepath.Base(path)
	// An optional role may retain comments after its last declaration is gone.
	allowEmpty := (role == "api.chik" || role == "clients.chik") &&
		len(tokens) == 1 && tokens[0].Kind == EOF && len(diags) == 0
	root := p.file(allowEmpty)
	attachTrivia(root, trivia)
	file := &File{Path: path, Root: root, Diagnostics: p.diags, Source: source, Tokens: tokens, Trivia: trivia}
	file.Diagnostics = append(file.Diagnostics, Check(file)...)
	file.Diagnostics = UniqueDiagnostics(file.Diagnostics)
	for index := range file.Diagnostics {
		if file.Diagnostics[index].File == "" {
			file.Diagnostics[index].File = path
		}
	}
	SortDiagnostics(file.Diagnostics)
	return file
}

func (p *parser) peek(offset int) Token {
	index := p.pos + offset
	if index >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[index]
}

func (p *parser) at() Token { return p.peek(0) }

func (p *parser) next() Token {
	token := p.at()
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return token
}

func (p *parser) atPunct(text string) bool {
	token := p.at()
	return token.Kind == Punct && token.Text == text
}

func (p *parser) atIdent(text string) bool {
	token := p.at()
	return token.Kind == Ident && token.Text == text
}

// optionalModifier reports whether the word at the cursor marks the preceding
// field as one a caller may omit. It has to share the line with the type, so a
// field that is itself named optional still begins a new entry.
func (p *parser) optionalModifier() bool {
	if !p.atIdent("optional") || p.pos == 0 {
		return false
	}
	return p.tokens[p.pos-1].Line == p.at().Line
}

func (p *parser) fail(code, path, message string) {
	token := p.at()
	p.diags = append(p.diags, Diagnostic{
		Code: code, Severity: "error", Path: path, Message: message,
		Range: Range{Start: Pos{Line: token.Line, Column: token.Column}, End: Pos{Line: token.Line, Column: token.Column + 1}},
	})
}

// endEntry consumes an entry terminator. A semicolon is accepted, and so is a
// line break, which is the canonical form: the decision makes a newline the end
// of an entry inside a block and has the formatter drop semicolons.
func (p *parser) endEntry(path string) {
	if p.atPunct(";") {
		p.next()
		return
	}
	if p.atPunct("}") || p.at().Kind == EOF {
		return
	}
	previous := p.tokens[p.pos-1]
	if p.at().Line != previous.Line {
		return
	}
	p.expect(";", path)
}

// expect consumes a punctuation token or reports it and keeps going. The
// diagnostic points just past the previous token, which is where the missing
// token belongs, rather than at the start of the next line.
func (p *parser) expect(text, path string) bool {
	if p.atPunct(text) {
		p.next()
		return true
	}
	var at Pos
	if p.pos > 0 {
		previous := p.tokens[p.pos-1]
		at = Pos{Line: previous.Line, Column: previous.Column + previous.width()}
	} else {
		current := p.at()
		at = Pos{Line: current.Line, Column: current.Column}
	}
	p.diags = append(p.diags, Diagnostic{
		Code: CodeExpectedToken, Severity: "error", Path: path,
		Message:    fmt.Sprintf("expected %q", text),
		Range:      Range{Start: at, End: Pos{Line: at.Line, Column: at.Column + 1}},
		Suggestion: p.suggestionAt(at, text),
	})
	return false
}

// suggestionAt returns the corrected line when inserting the missing token is
// the only reading. A semicolon has one place it can go, so the reader can
// paste the line back. Other tokens depend on what the author meant, so no
// suggestion is offered.
func (p *parser) suggestionAt(at Pos, text string) *Suggestion {
	if text != ";" {
		return nil
	}
	lines := strings.Split(p.src, "\n")
	if at.Line < 1 || at.Line > len(lines) {
		return nil
	}
	runes := []rune(lines[at.Line-1])
	// The grammar accepts CRLF, and a trailing CR is part of the line ending.
	if len(runes) > 0 && runes[len(runes)-1] == '\r' {
		runes = runes[:len(runes)-1]
	}
	index := min(max(0, at.Column-1), len(runes))
	corrected := string(runes[:index]) + text + string(runes[index:])
	return &Suggestion{Text: corrected}
}

// takeName consumes an identifier and reports when the position needs one.
func (p *parser) takeName(path string) (Token, bool) {
	if p.at().Kind == Ident {
		return p.next(), true
	}
	p.fail(CodeExpectedName, path, "expected a name")
	return p.at(), false
}

// nodeRange spans from a node's first token to its last, excluding trivia.
func nodeRange(start, end Token) Range {
	return Range{
		Start: Pos{Line: start.Line, Column: start.Column},
		End:   Pos{Line: end.Line, Column: end.Column + end.width()},
	}
}

func (p *parser) file(allowEmpty bool) *Node {
	root := &Node{Kind: NodeFile}
	seen := map[NodeKind]bool{}
	for p.at().Kind != EOF {
		block := p.block()
		if block == nil {
			// block reports its own problem and consumes a token so this ends.
			continue
		}
		if seen[block.Kind] {
			p.diags = append(p.diags, Diagnostic{
				Code: CodeDuplicateBlock, Severity: "error", Path: sourceName(block.Kind),
				Message: "this block appears more than once", Range: block.NameRange,
			})
		}
		seen[block.Kind] = true
		root.Children = append(root.Children, block)
	}
	if len(root.Children) == 0 && !allowEmpty {
		p.diags = append(p.diags, Diagnostic{
			Code: CodeEmptyFile, Severity: "error", Message: "the file declares no block",
			Range: Range{Start: Pos{Line: 1, Column: 1}, End: Pos{Line: 1, Column: 1}},
		})
	}
	root.Range = Range{Start: Pos{Line: 1, Column: 1}, End: p.endPos()}
	return root
}

func (p *parser) endPos() Pos {
	token := p.at()
	return Pos{Line: token.Line, Column: token.Column}
}

var blockKinds = map[string]NodeKind{
	"project":    NodeProjectBlock,
	"database":   NodeSchemaBlock,
	"services":   NodeServicesBlock,
	"types":      NodeTypesBlock,
	"routes":     NodeRoutesBlock,
	"clients":    NodeClientsBlock,
	"auth":       NodeAuthBlock,
	"extensions": NodeExtensionsBlock,
}

// block parses one top-level block. A name it does not know is an error, not a
// skip: silently dropping a block would hide a typo.
func (p *parser) block() *Node {
	token := p.at()
	if token.Kind != Ident {
		p.fail(CodeExpectedName, "", "expected a block name")
		p.next()
		return nil
	}
	kind, ok := blockKinds[token.Text]
	if !ok {
		p.fail(CodeExpectedName, "", fmt.Sprintf("unknown block %q", token.Text))
		p.next()
		return nil
	}
	start := p.next()
	node := &Node{Kind: kind, NameRange: nodeRange(start, start)}
	if !p.expect("{", sourceName(kind)) {
		return node
	}
	switch kind {
	case NodeServicesBlock:
		for p.at().Kind == Ident {
			node.Children = append(node.Children, p.service())
		}
	case NodeSchemaBlock:
		for p.at().Kind == Ident {
			if p.peek(1).Kind == Punct && p.peek(1).Text == "{" {
				node.Children = append(node.Children, p.nestedBlock(sourceName(kind)))
				continue
			}
			node.Children = append(node.Children, p.entries(sourceName(kind))...)
		}
	case NodeRoutesBlock:
		for p.at().Kind == Ident {
			node.Children = append(node.Children, p.route(sourceName(kind)))
		}
		p.outsideBeta(node, "a routes block is not part of the alpha grammar")
	case NodeTypesBlock:
		for p.at().Kind == Ident {
			node.Children = append(node.Children, p.typeDecl(sourceName(kind)))
		}
	case NodeExtensionsBlock:
		// extensions.chik is retired: the control-plane selection is a
		// capabilities key in project.chik. The block is parsed so the file still
		// yields positions, then refused.
		node.Children = append(node.Children, p.entries(sourceName(kind))...)
		p.outsideBeta(node, "an extensions block is retired; declare capabilities in project.chik")
	default:
		node.Children = append(node.Children, p.entries(sourceName(kind))...)
	}
	end := p.at()
	p.expect("}", sourceName(kind))
	node.Range = nodeRange(start, end)
	return node
}

// entries parses `name: value;` pairs, which is how most blocks are written.
func (p *parser) entries(path string) []*Node {
	var nodes []*Node
	for p.at().Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == ":" {
		start := p.at()
		name := p.next().Text
		p.next()
		value := p.value(path + "." + name)
		entry := &Node{Kind: NodeEntry, Name: name, Children: value, Range: nodeRange(start, p.tokens[p.pos-1])}
		p.endEntry(path + "." + name)
		nodes = append(nodes, entry)
	}
	return nodes
}

// value parses a value after a colon: a string, a name, a list, a record, or a
// nested block.
func (p *parser) value(path string) []*Node {
	token := p.at()
	switch {
	case token.Kind == String:
		p.next()
		return []*Node{{Kind: NodeTypeRef, Tokens: []Token{token}, Range: nodeRange(token, token)}}
	case token.Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == "{":
		start := p.next()
		p.next()
		node := &Node{Kind: NodeTypeRecord, Name: start.Text}
		node.Children = p.entries(path)
		end := p.at()
		p.expect("}", path)
		node.Range = nodeRange(start, end)
		return []*Node{node}
	case token.Kind == Ident:
		p.next()
		return []*Node{{Kind: NodeTypeRef, Name: token.Text, Tokens: []Token{token}, Range: nodeRange(token, token)}}
	case token.Kind == Punct && token.Text == "[":
		start := p.next()
		node := &Node{Kind: NodeTypeList}
		for p.at().Kind != EOF && !p.atPunct("]") {
			node.Children = append(node.Children, p.value(path)...)
			if p.atPunct(",") {
				p.next()
				continue
			}
			break
		}
		end := p.at()
		p.expect("]", path)
		node.Range = nodeRange(start, end)
		return []*Node{node}
	case token.Kind == Punct && token.Text == "{":
		start := p.next()
		node := &Node{Kind: NodeTypeRecord}
		node.Children = p.inlineEntries(path)
		end := p.at()
		p.expect("}", path)
		node.Range = nodeRange(start, end)
		return []*Node{node}
	case token.Kind == Number:
		p.next()
		return []*Node{{Kind: NodeTypeRef, Name: token.Text, Tokens: []Token{token}, Range: nodeRange(token, token)}}
	}
	p.fail(CodeExpectedType, path, "expected a value")
	p.next()
	return nil
}

// service parses `name { operations { ... } }`.
func (p *parser) service() *Node {
	start := p.next()
	node := &Node{Kind: NodeService, Name: start.Text}
	if !p.expect("{", "services."+start.Text) {
		return node
	}
	for p.at().Kind == Ident {
		key := p.at()
		if key.Text == "operations" && p.peek(1).Kind == Punct && p.peek(1).Text == "{" {
			p.next()
			p.next()
			for p.at().Kind == Ident {
				node.Children = append(node.Children, p.operation("services."+start.Text))
			}
			p.expect("}", "services."+start.Text+".operations")
			continue
		}
		p.fail(CodeExpectedName, "services."+start.Text, "expected operations")
		p.next()
	}
	end := p.at()
	p.expect("}", "services."+start.Text)
	node.Range = nodeRange(start, end)
	return node
}

// operation parses `name(params) -> { fields };`.
func (p *parser) operation(path string) *Node {
	start := p.next()
	operation := &Node{Kind: NodeOperation, Name: start.Text}
	pricePath := path + ".operations." + start.Text + ".price"
	path = path + "." + start.Text
	p.expect("(", path)
	for p.at().Kind == Ident {
		paramStart := p.next()
		param := &Node{Kind: NodeParam, Name: paramStart.Text}
		if p.expect(":", path+"."+paramStart.Text) {
			param.Children = p.typeExpr(path+"."+paramStart.Text, true)
		}
		if p.optionalModifier() {
			p.next()
			param.Optional = true
		}
		param.Range = nodeRange(paramStart, p.at())
		operation.Children = append(operation.Children, param)
		if p.atPunct(",") {
			p.next()
			continue
		}
		break
	}
	p.expect(")", path)
	if !(p.atPunct("-") && p.peek(1).Kind == Punct && p.peek(1).Text == ">") {
		p.fail(CodeExpectedToken, path, "expected ->")
		p.skipToSemicolon()
		return operation
	}
	p.next()
	p.next()
	if p.atPunct("{") {
		recordStart := p.next()
		result := &Node{Kind: NodeResult}
		result.Children = p.fields(path, false)
		recordEnd := p.at()
		p.expect("}", path)
		result.Range = nodeRange(recordStart, recordEnd)
		operation.Children = append(operation.Children, result)
		if p.atIdent("price") && p.at().Line == recordEnd.Line {
			operation.Children = append(operation.Children, p.operationPrice(pricePath))
		}
	}
	operation.Range = nodeRange(start, p.tokens[p.pos-1])
	p.endEntry(path)
	return operation
}

func (p *parser) skipToSemicolon() {
	for p.at().Kind != EOF && !p.atPunct(";") {
		p.next()
	}
	p.expect(";", "")
}

// fields parses `name: type` pairs inside a result record.
func (p *parser) fields(path string, input bool) []*Node {
	var nodes []*Node
	for p.at().Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == ":" {
		start := p.next()
		p.next()
		field := &Node{Kind: NodeField, Name: start.Text}
		field.Children = p.typeExpr(path+"."+start.Text, input)
		// `optional` after a type means the field may be omitted, which is a
		// different thing from a nullable value. Only an input field can be
		// omitted, so the compiler's rule is reported here rather than left for
		// the YAML it renders.
		if p.optionalModifier() {
			modifier := p.next()
			if input {
				field.Optional = true
			} else {
				p.diags = append(p.diags, Diagnostic{
					Code: CodeInvalidModifier, Severity: "error", Path: path + "." + start.Text,
					Message: "optional is allowed only on input fields",
					Range:   nodeRange(modifier, modifier),
				})
			}
		}
		field.Range = nodeRange(start, p.at())
		nodes = append(nodes, field)
		if p.atPunct(",") {
			p.next()
			continue
		}
		break
	}
	return nodes
}

// typeExpr parses a type with its trailing modifiers.
func (p *parser) typeExpr(path string, input bool) []*Node {
	token := p.at()
	var nodes []*Node
	switch {
	case token.Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == "<":
		if token.Text == "Page" || token.Text == "map" {
			p.diags = append(p.diags, Diagnostic{
				Code: CodeOutsideBeta, Severity: "error", Path: path,
				Message: token.Text + " is not part of the alpha grammar",
				Range:   nodeRange(token, token),
			})
		}
		p.next()
		p.next()
		inner := p.typeExpr(path, input)
		p.expect(">", path)
		nodes = append(nodes, &Node{Kind: NodeTypeRef, Name: token.Text, Path: path, Children: inner, Range: nodeRange(token, p.at())})
	case token.Kind == Ident:
		p.next()
		nodes = append(nodes, &Node{Kind: NodeTypeRef, Name: token.Text, Path: path, Tokens: []Token{token}, Range: nodeRange(token, token)})
	case p.atPunct("{"):
		start := p.next()
		record := &Node{Kind: NodeTypeRecord}
		record.Children = p.fields(path, input)
		end := p.at()
		p.expect("}", path)
		record.Range = nodeRange(start, end)
		nodes = append(nodes, record)
	default:
		p.fail(CodeExpectedType, path, "expected a type")
		p.next()
		return nil
	}
	for {
		switch {
		case p.atPunct("["):
			p.next()
			end := p.at()
			p.expect("]", path)
			nodes = append(nodes, &Node{Kind: NodeTypeList, Children: nodes, Range: nodeRange(token, end)})
		case p.atPunct("?"):
			end := p.next()
			nodes = append(nodes, &Node{Kind: NodeTypeNullable, Children: nodes, Range: nodeRange(token, end)})
		default:
			return nodes
		}
	}
}

// typeDecl parses `Name = { ... };`, the only form a beta types block uses.
func (p *parser) typeDecl(path string) *Node {
	start := p.next()
	node := &Node{Kind: NodeTypeRef, Name: start.Text, Path: path + "." + start.Text, NameRange: nodeRange(start, start)}
	if !p.expect("=", path+"."+start.Text) {
		return node
	}
	if p.atIdent("union") && p.peek(1).Kind == Punct && p.peek(1).Text == "(" {
		unionToken := p.at()
		p.next()
		p.next()
		if p.at().Kind == Ident {
			p.next()
		}
		p.expect(")", path+"."+start.Text)
		if p.expect("{", path+"."+start.Text) {
			variants := 0
			for p.at().Kind == Ident {
				p.next()
				p.expect(":", path+"."+start.Text)
				p.typeExpr(path+"."+start.Text, true)
				p.endEntry(path + "." + start.Text)
				variants++
			}
			p.expect("}", path+"."+start.Text)
			if variants < 2 {
				p.diags = append(p.diags, Diagnostic{
					Code: CodeUnionVariant, Severity: "error", Path: path + "." + start.Text,
					Message: "a union needs at least two variants", Range: nodeRange(unionToken, unionToken),
				})
			}
		}
		node.Range = nodeRange(start, p.tokens[p.pos-1])
		p.endEntry(path + "." + start.Text)
		return node
	}
	// Retain modifiers until the checker knows where the type is used.
	node.Children = p.typeExpr(path+"."+start.Text, true)
	node.Range = nodeRange(start, p.tokens[p.pos-1])
	p.endEntry(path + "." + start.Text)
	return node
}

// route parses `name METHOD "path";`.
func (p *parser) route(path string) *Node {
	start := p.next()
	node := &Node{Kind: NodeEntry, Name: start.Text}
	if p.at().Kind == Ident {
		method := p.next()
		node.Children = append(node.Children, &Node{Kind: NodeTypeRef, Name: method.Text, Tokens: []Token{method}, Range: nodeRange(method, method)})
	} else {
		p.fail(CodeInvalidMethod, path+"."+start.Text, "expected a method")
	}
	if p.at().Kind == String {
		quote := p.next()
		node.Children = append(node.Children, &Node{Kind: NodeTypeRef, Name: quote.Text, Tokens: []Token{quote}, Range: nodeRange(quote, quote)})
	} else {
		p.fail(CodeInvalidPath, path+"."+start.Text, "expected a quoted path")
	}
	p.endEntry(path + "." + start.Text)
	node.Range = nodeRange(start, p.at())
	return node
}

// sourceName is the block keyword as written, which is also the path prefix a
// diagnostic uses. A diagnostic must never show an internal node kind.
func sourceName(kind NodeKind) string {
	switch kind {
	case NodeProjectBlock:
		return "project"
	case NodeSchemaBlock:
		return "database"
	case NodeServicesBlock:
		return "services"
	case NodeTypesBlock:
		return "types"
	case NodeRoutesBlock:
		return "routes"
	case NodeClientsBlock:
		return "clients"
	case NodeAuthBlock:
		return "auth"
	case NodeExtensionsBlock:
		return "extensions"
	}
	return string(kind)
}

// inlineEntries parses `name: value, name: value` inside a record used as a
// value, where entries separate with commas rather than statements.
func (p *parser) inlineEntries(path string) []*Node {
	var nodes []*Node
	for p.at().Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == ":" {
		start := p.next()
		p.next()
		entry := &Node{Kind: NodeEntry, Name: start.Text, Path: path + "." + start.Text}
		entry.Children = p.value(path + "." + start.Text)
		entry.Range = nodeRange(start, p.tokens[p.pos-1])
		nodes = append(nodes, entry)
		if p.atPunct(",") {
			p.next()
			continue
		}
		break
	}
	return nodes
}

// nestedBlock parses `name { ... }` inside a block, where the body holds
// further entries or nested blocks.
func (p *parser) nestedBlock(path string) *Node {
	start := p.next()
	p.next()
	node := &Node{Kind: NodeEntry, Name: start.Text, Path: path + "." + start.Text}
	for p.at().Kind == Ident {
		if p.peek(1).Kind == Punct && p.peek(1).Text == "{" {
			node.Children = append(node.Children, p.nestedBlock(path+"."+start.Text))
			continue
		}
		entries := p.blockEntries(path + "." + start.Text)
		if len(entries) == 0 {
			break
		}
		node.Children = append(node.Children, entries...)
		continue
	}
	end := p.at()
	p.expect("}", path+"."+start.Text)
	node.Range = nodeRange(start, end)
	return node
}

// blockEntries parses a nested block body. An entry may end with a semicolon or
// a comma, or with nothing when it is the last one on its line. The corpus
// decides which form a given example uses.
func (p *parser) blockEntries(path string) []*Node {
	var nodes []*Node
	for p.at().Kind == Ident && p.peek(1).Kind == Punct && p.peek(1).Text == ":" {
		start := p.next()
		p.next()
		entry := &Node{Kind: NodeEntry, Name: start.Text, Path: path + "." + start.Text}
		entry.Children = p.value(path + "." + start.Text)
		entry.Range = nodeRange(start, p.tokens[p.pos-1])
		nodes = append(nodes, entry)
		if p.atPunct(";") || p.atPunct(",") {
			p.next()
			continue
		}
		continue
	}
	return nodes
}

// outsideBeta reports a construct the language defines but the beta does not
// accept. It points at the block keyword, which is what the reader has to
// remove or replace.
func (p *parser) outsideBeta(node *Node, message string) {
	p.diags = append(p.diags, Diagnostic{
		Code: CodeOutsideBeta, Severity: "error", Path: sourceName(node.Kind),
		Message: message, Range: node.NameRange,
	})
}
