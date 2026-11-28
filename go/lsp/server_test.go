package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// run feeds framed messages to the server and returns the replies it wrote.
// It exercises the same path an editor uses: stdio frames in, frames out.
func run(t *testing.T, messages ...string) []Message {
	t.Helper()
	var input bytes.Buffer
	for _, message := range messages {
		body := []byte(message)
		input.WriteString("Content-Length: " + itoa(len(body)) + "\r\n\r\n")
		input.Write(body)
	}
	var output bytes.Buffer
	if err := Serve(&input, &output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	reader := bufio.NewReader(&output)
	var replies []Message
	for {
		message, err := readMessage(reader)
		if err != nil {
			break
		}
		replies = append(replies, *message)
	}
	return replies
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestInitializeAdvertisesTheFeaturesAnEditorNeeds(t *testing.T) {
	replies := run(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	result := string(replies[0].Result)
	for _, want := range []string{"definitionProvider", "documentFormattingProvider", "completionProvider", "textDocumentSync"} {
		if !strings.Contains(result, want) {
			t.Errorf("initialize result is missing %s: %s", want, result)
		}
	}
}

func TestOpenPublishesTheParserDiagnostics(t *testing.T) {
	document := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/api.chik","version":1,"text":"types {\n  Post = { author: Author }\n}\n"}}}`
	replies := run(t, document)
	if len(replies) != 1 || replies[0].Method != "textDocument/publishDiagnostics" {
		t.Fatalf("replies = %+v", replies)
	}
	var params struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(replies[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Diagnostics) == 0 {
		t.Fatal("a missing semicolon should report a diagnostic")
	}
	// The code comes from chiklang, so it is the same number chik check prints.
	// CHK0301 is the unknown-type number, the same one chik check prints.
	if params.Diagnostics[0].Code != "CHK0301" {
		t.Errorf("code = %q, want CHK0301", params.Diagnostics[0].Code)
	}
	if params.Diagnostics[0].Range.Start.Line != 1 {
		t.Errorf("start line = %d, want 1 (zero-based)", params.Diagnostics[0].Range.Start.Line)
	}
}

func TestOpenOptionalRoleWithoutDeclarations(t *testing.T) {
	for _, test := range []struct {
		name, file, source string
		want               []string
	}{
		{name: "client notes", file: "clients.chik", source: "// Client notes\n"},
		{name: "API notes", file: "api.chik", source: "/* API notes */\n"},
		{name: "empty API", file: "api.chik"},
		{name: "blank clients", file: "clients.chik", source: " \t\r\n"},
		{name: "required project", file: "project.chik", source: "// Project notes\n", want: []string{"CHK0105"}},
		{name: "unclosed comment", file: "clients.chik", source: "/* Client notes", want: []string{"CHK0004", "CHK0105"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			open := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"version":1,"text":%q}}}`,
				"file:///p/chik/"+test.file, test.source)
			replies := run(t, open)
			if len(replies) != 1 || replies[0].Method != "textDocument/publishDiagnostics" {
				t.Fatalf("replies = %+v", replies)
			}
			var published struct {
				Diagnostics []Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(replies[0].Params, &published); err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, diagnostic := range published.Diagnostics {
				codes = append(codes, diagnostic.Code)
			}
			if !slices.Equal(codes, test.want) {
				t.Fatalf("diagnostic codes = %v, want %v", codes, test.want)
			}
		})
	}
}

func TestDefinitionFindsADeclaredType(t *testing.T) {
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/api.chik","version":1,"text":"types {\n  Post = { id: id }\n}\nservices {\n  posts {\n    operations {\n      get() -> { post: Post }\n    }\n  }\n}\n"}}}`
	definition := `{"jsonrpc":"2.0","id":2,"method":"textDocument/definition","params":{"textDocument":{"uri":"file:///p/chik/api.chik"},"position":{"line":6,"character":23}}}`
	replies := run(t, open, definition)
	var result []Location
	for _, reply := range replies {
		if string(reply.ID) == "2" {
			if err := json.Unmarshal(reply.Result, &result); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(result) != 1 {
		t.Fatalf("definition = %+v, want one location", result)
	}
	if result[0].Range.Start.Line != 1 {
		t.Errorf("definition line = %d, want 1 (the Post declaration)", result[0].Range.Start.Line)
	}
}

func TestCompletionOffersTypesAndBuiltins(t *testing.T) {
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/api.chik","version":1,"text":"types {\n  Post = { id: id }\n}\n"}}}`
	completion := `{"jsonrpc":"2.0","id":3,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///p/chik/api.chik"},"position":{"line":1,"character":2}}}`
	replies := run(t, open, completion)
	var list CompletionList
	for _, reply := range replies {
		if string(reply.ID) == "3" {
			if err := json.Unmarshal(reply.Result, &list); err != nil {
				t.Fatal(err)
			}
		}
	}
	labels := map[string]bool{}
	for _, item := range list.Items {
		labels[item.Label] = true
	}
	for _, want := range []string{"Post", "id", "string", "timestamp"} {
		if !labels[want] {
			t.Errorf("completion is missing %q: %+v", want, labels)
		}
	}
}

func TestFormattingReturnsTheFormatterOutput(t *testing.T) {
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/api.chik","version":1,"text":"types {\n    Post = { id: id }\n}\n"}}}`
	format := `{"jsonrpc":"2.0","id":4,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///p/chik/api.chik"},"options":{}}}`
	replies := run(t, open, format)
	var edits []TextEdit
	for _, reply := range replies {
		if string(reply.ID) == "4" {
			if err := json.Unmarshal(reply.Result, &edits); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %+v, want one", edits)
	}
	if edits[0].NewText != "types {\n  Post = { id: id }\n}\n" {
		t.Errorf("formatted text = %q", edits[0].NewText)
	}
}

// A document being edited often has an unclosed string or a stray character.
// The lexer drops those bytes, so formatting it on save used to replace the
// document with text that no longer held what the user typed.
func TestFormattingLeavesADocumentWithLexerErrorsAlone(t *testing.T) {
	for _, text := range []string{
		"project {\\n  name: \\\"dem\\n}\\n",
		"project {\\n  name: \\\"demo\\\" # note\\n}\\n",
	} {
		open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/project.chik","version":1,"text":"` + text + `"}}}`
		format := `{"jsonrpc":"2.0","id":5,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///p/chik/project.chik"},"options":{}}}`
		var edits []TextEdit
		found := false
		for _, reply := range run(t, open, format) {
			if string(reply.ID) == "5" {
				found = true
				if err := json.Unmarshal(reply.Result, &edits); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !found {
			t.Fatalf("no formatting reply for %s", text)
		}
		if len(edits) != 0 {
			t.Errorf("formatting %s returned edits %+v, want none", text, edits)
		}
	}
}

func TestUnknownRequestGetsAnErrorReply(t *testing.T) {
	replies := run(t, `{"jsonrpc":"2.0","id":9,"method":"workspace/unknown","params":{}}`)
	if len(replies) != 1 || replies[0].Error == nil {
		t.Fatalf("replies = %+v, want one error", replies)
	}
	if replies[0].Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", replies[0].Error.Code)
	}
}

func TestOpenDiagnosesOperationPrices(t *testing.T) {
	for _, test := range []struct {
		name     string
		price    string
		message  string
		severity int
	}{
		{name: "exact minimum", price: "0.50 usd", message: "not supported", severity: 1},
		{name: "exactly below minimum", price: "0.49999999999999999999 usd", message: "$0.50", severity: 2},
		{name: "invalid number", price: "0.5.0 usd", message: "positive decimal", severity: 1},
		{name: "invalid currency", price: "0.50 uSd", message: "lowercase currency", severity: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "services { catalog { operations { search() -> {} price " + test.price + " } } }"
			open := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///p/chik/api.chik","version":1,"text":%q}}}`, source)
			replies := run(t, open)
			if len(replies) != 1 || replies[0].Method != "textDocument/publishDiagnostics" {
				t.Fatalf("replies = %+v", replies)
			}
			var params struct {
				Diagnostics []Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(replies[0].Params, &params); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range params.Diagnostics {
				if diagnostic.Severity == test.severity && strings.Contains(diagnostic.Message, test.message) &&
					strings.Contains(diagnostic.Path, "services.catalog.operations.search.price") {
					found = true
				}
				if test.name == "exact minimum" && diagnostic.Severity == 2 {
					t.Errorf("minimum payment unexpectedly warned: %+v", diagnostic)
				}
			}
			if !found {
				t.Fatalf("missing price diagnostic for %q: %+v", test.price, params.Diagnostics)
			}
		})
	}
}
