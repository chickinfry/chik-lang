package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/chickinfry/chik-lang/go"
)

// ExitError is returned by Serve when the client sent exit without a shutdown
// first. LSP 3.17 says the process then exits with code 1; a shutdown before
// exit exits with 0, which Serve reports by returning nil.
var ExitError = errors.New("exit without shutdown")

// server holds the open documents. The LSP connection is single-threaded per
// message, so no lock is needed: the read loop handles one message at a time.
type server struct {
	documents map[string]*document
	// shutdown records that the client asked the server to shut down. After
	// that, a request is an error and only exit is expected.
	shutdown bool
}

// Serve reads messages from in and writes replies to out until the client
// sends exit or the stream closes.
func Serve(in io.Reader, out io.Writer) error {
	s := &server{documents: map[string]*document{}}
	reader := bufio.NewReader(in)
	for {
		message, err := readMessage(reader)
		if err == io.EOF {
			// The client closed the stream without exit. That is a clean end for the
			// process, though the protocol prefers an explicit exit.
			return nil
		}
		if err != nil {
			// A frame that cannot be decoded is a parse error, not a reason to die. The
			// id is unknown at that point, so the reply carries a null id, which is what
			// JSON-RPC 2.0 prescribes for a message that could not be read.
			if writeErr := writeMessage(out, Message{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &ResponseError{Code: -32700, Message: "parse error: " + err.Error()}}); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err := s.handle(message, out); err != nil {
			if errors.Is(err, errExit) {
				if s.shutdown {
					return nil
				}
				return ExitError
			}
			return err
		}
	}
}

// errExit unwinds Serve after the exit notification. It is not an error a
// caller sees; Serve turns it into a nil or ExitError result.
var errExit = errors.New("exit")

// handle dispatches one message. A request gets a result or an error; a
// notification gets no reply, which is why the two are separated here.
func (s *server) handle(message *Message, out io.Writer) error {
	switch message.Method {
	case "initialize":
		return s.reply(out, message.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           1,
				"definitionProvider":         true,
				"documentFormattingProvider": true,
				"completionProvider":         map[string]any{"triggerCharacters": []string{":", " ", "."}},
			},
			"serverInfo": map[string]any{"name": "chik", "version": "1"},
		})
	case "initialized":
		return nil
	case "shutdown":
		s.shutdown = true
		return s.reply(out, message.ID, nil)
	case "exit":
		return errExit
	default:
		// After shutdown a request is invalid, which LSP 3.17 requires.
		if s.shutdown && message.ID != nil {
			return s.replyError(out, message.ID, -32600, "the server has shut down")
		}
		switch message.Method {
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
					Text    string `json:"text"`
				} `json:"textDocument"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				return s.badParams(out, message)
			}
			s.documents[params.TextDocument.URI] = newDocument(params.TextDocument.URI, params.TextDocument.Version, params.TextDocument.Text)
			return s.publish(out, params.TextDocument.URI)
		case "textDocument/didChange":
			var params struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				return s.badParams(out, message)
			}
			doc := s.documents[params.TextDocument.URI]
			if doc == nil {
				return nil
			}
			if len(params.ContentChanges) > 0 {
				// A full-sync client sends the whole text. This server advertises sync kind
				// 1 (full), so the last change is the document.
				doc.update(params.TextDocument.Version, params.ContentChanges[len(params.ContentChanges)-1].Text)
			}
			return s.publish(out, params.TextDocument.URI)
		case "textDocument/didClose":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				return s.badParams(out, message)
			}
			delete(s.documents, params.TextDocument.URI)
			return s.notify(out, "textDocument/publishDiagnostics", map[string]any{
				"uri": params.TextDocument.URI, "diagnostics": []Diagnostic{},
			})
		case "textDocument/definition":
			return s.definition(out, message)
		case "textDocument/completion":
			return s.completion(out, message)
		case "textDocument/formatting":
			return s.formatting(out, message)
		}
		// An unknown request still needs a reply, or the client waits forever. An
		// unknown notification is ignored, which the protocol allows.
		if message.ID != nil {
			return s.replyError(out, message.ID, -32601, "method not found: "+message.Method)
		}
		return nil
	}
}

// badParams answers a request whose params do not decode. A notification has no
// id, so there is nothing to answer and the message is dropped.
func (s *server) badParams(out io.Writer, message *Message) error {
	if message.ID == nil {
		return nil
	}
	return s.replyError(out, message.ID, -32602, "invalid params")
}

// publish parses a document and sends its diagnostics.
func (s *server) publish(out io.Writer, uri string) error {
	return s.notify(out, "textDocument/publishDiagnostics", map[string]any{
		"uri": uri, "diagnostics": s.diagnosticsFor(uri),
	})
}

// diagnosticsFor is the one place a document becomes protocol diagnostics.
// The content is chiklang.Report, which is what chik check --format json
// prints, so an editor and the command line cannot report different things.
func (s *server) diagnosticsFor(uri string) []Diagnostic {
	doc := s.documents[uri]
	if doc == nil {
		return []Diagnostic{}
	}
	file := chiklang.Parse(pathOf(uri), doc.text)
	report := chiklang.Report(file)
	diagnostics := make([]Diagnostic, 0, len(report.Diagnostics))
	for _, entry := range report.Diagnostics {
		severity := 1
		if entry.Severity == "warning" {
			severity = 2
		}
		diagnostics = append(diagnostics, Diagnostic{
			Range: Range{
				Start: doc.position(entry.Range.Start.Line, entry.Range.Start.Column),
				End:   doc.position(entry.Range.End.Line, entry.Range.End.Column),
			},
			Severity:   severity,
			Code:       entry.Code,
			Source:     "chik",
			Message:    entry.Message,
			Path:       entry.Path,
			SourceLine: entry.SourceLine,
			Help:       entry.Help,
			Notes:      entry.Notes,
		})
	}
	return diagnostics
}

func (s *server) reply(out io.Writer, id json.RawMessage, result any) error {
	return writeMessage(out, Message{JSONRPC: "2.0", ID: id, Result: mustJSON(result)})
}

func (s *server) replyError(out io.Writer, id json.RawMessage, code int, message string) error {
	return writeMessage(out, Message{JSONRPC: "2.0", ID: id, Error: &ResponseError{Code: code, Message: message}})
}

func (s *server) notify(out io.Writer, method string, params any) error {
	return writeMessage(out, Message{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
}

func mustJSON(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage("null")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

// pathOf turns a file URI into the path a diagnostic names. A non-file URI is
// returned unchanged so the message is still useful.
func pathOf(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return uri
	}
	return parsed.Path
}

// mustMessage decodes the params of a request, or returns an error reply.
func mustMessage(message *Message, target any) error {
	if len(message.Params) == 0 {
		return fmt.Errorf("missing params")
	}
	return json.Unmarshal(message.Params, target)
}
