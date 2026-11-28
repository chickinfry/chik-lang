package chiklang

import (
	"strings"
	"testing"
)

// TestConvertRefusesLoss covers the constructs with no .chik form. Dropping
// one would leave a converted file that looks complete but is not the same
// declaration.
func TestConvertRefusesLoss(t *testing.T) {
	cases := []struct{ name, role, source, want string }{
		{"one_of group", "api", "services:\n  s:\n    operations:\n      o:\n        input:\n          rows:\n            type: list\n            optional: true\n            items:\n              a: {type: string, optional: true}\n              b: {type: string, optional: true}\n            one_of: {x: [a, b]}\n", "one_of"},
		{"input default", "api", "services:\n  s:\n    operations:\n      o:\n        input:\n          a: {type: string, optional: true, default: \"x\"}\n", "default"},
		{"public selector", "api", "services:\n  s:\n    operations:\n      o:\n        public: true\n", "public"},
		{"operation method", "api", "services:\n  s:\n    operations:\n      o:\n        method: GET\n", "method"},
		{name: "operation price", role: "api", source: "services:\n  s:\n    operations:\n      o:\n        price: { amount: 0.50, currency: usd }\n", want: "price"},
		{"not a mapping", "project", "- one\n", "not a mapping"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, diagnostics := Convert(test.role, []byte(test.source))
			if len(diagnostics) == 0 {
				t.Fatalf("converted without refusing:\n%s", output)
			}
			if output != "" {
				t.Fatalf("a refusal must not return a partial result: %q", output)
			}
			if diagnostics[0].Code != CodeConvertUnsupported {
				t.Fatalf("code = %q, want %q", diagnostics[0].Code, CodeConvertUnsupported)
			}
			if !strings.Contains(diagnostics[0].Message, test.want) {
				t.Fatalf("message = %q, want it to mention %q", diagnostics[0].Message, test.want)
			}
		})
	}
}

// TestConvertKeepsColumnDefault keeps the other half: a database column
// default has a form, so it must convert rather than be refused.
func TestConvertKeepsColumnDefault(t *testing.T) {
	source := "database:\n  profile: postgresql\n  tables:\n    t:\n      columns:\n        id: {type: id, primary_key: true, default: {kind: uuid}}\n"
	output, diagnostics := Convert("schema", []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("refused: %+v", diagnostics)
	}
	if !strings.Contains(output, "default: { kind: uuid }") {
		t.Fatalf("default lost:\n%s", output)
	}
}

func TestConvertKeepsNamesThatMatchSettings(t *testing.T) {
	for _, test := range []struct {
		role   string
		source string
	}{
		{"api", `services:
  public:
    operations:
      offline:
        input:
          default: {type: string}
          payload:
            type: object
            fields:
              method: {type: string}
              one_of: {type: string}
        output:
          public: {type: string}
          rows:
            type: list
            items:
              offline: {type: string}
              default: {type: string}
              payload: {type: object, fields: {public: {type: string}}}
`},
		{"project", `project:
  name: demo
generation:
  server:
    language: typescript
  clients: [go, typescript]
storage:
  method: s3
vault: chickinfry
`},
	} {
		t.Run(test.role, func(t *testing.T) {
			source := []byte(test.source)
			converted, diagnostics := Convert(test.role, source)
			if len(diagnostics) != 0 {
				t.Fatalf("refused: %+v", diagnostics)
			}
			assertRenderedYAML(t, test.role, converted, source)
		})
	}
}
