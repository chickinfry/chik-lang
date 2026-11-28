package chiklang

import (
	_ "embed"
	"regexp"
	"strings"
)

// Embed the published diagnostic catalog so it is available offline.
//
//go:embed spec/error-codes.md
var publishedCodes string

// Explanation is the published description of one diagnostic number. The
// number, group, and summary come from spec/error-codes.md in
// this repository, which is the canonical list and is
// embedded below. The body, help line, and notes are this implementation's
// wording, because the specification fixes what a number means, not how a tool
// phrases it.
type Explanation struct {
	Code    string
	Group   string
	Summary string
	Help    string
	Notes   []string
	Body    string
}

// wording is this implementation's text for a number. It is keyed by the code
// constant so a rename cannot silently drop a description.
type wording struct {
	Code  string
	Help  string
	Notes []string
	Body  string
}

// wordings is ordered by number so a reader and a lookup agree.
var wordings = []wording{
	{
		Code: CodeUnexpectedCharacter,
		Help: "remove the character or write it inside a string",
		Body: "A character appears where the lexer cannot start a token. The grammar accepts identifiers, string literals, and the punctuation it names.\n\nIf the character is meant as data, put it inside a double-quoted string.",
	},
	{
		Code: CodeUnterminatedString,
		Help: "close the string with a double quote on the same line",
		Body: "A string literal reaches the end of its line or the end of the file without a closing double quote.\n\nA string stays on one line. Use \\n inside the string for a line break.",
	},
	{
		Code: CodeInvalidEscape,
		Help: "use a JSON escape such as \\n or a doubled backslash",
		Body: "A backslash is followed by a character that is not part of a JSON escape.\n\nThe accepted escapes are the ones JSON defines, so a Windows path needs a doubled backslash.",
	},
	{
		Code: CodeUnterminatedComment,
		Help: "close the block comment with */",
		Body: "A block comment starts with /* and reaches the end of the file without its closing */.\n\nEverything after the opening marker is treated as a comment, so the rest of the file is not read.",
	},
	{
		Code: CodeNestedComment,
		Help: "end the first comment before starting another",
		Body: "A block comment contains another /* before its closing */. Block comments do not nest.\n\nRemove the inner markers or move that text outside the comment.",
	},
	{
		Code: CodeExpectedToken,
		Help: "add the token the message names",
		Body: "A required punctuation token is missing or is a different token. The message names the token the grammar expects at that position.\n\nA missing semicolon is the most common cause, because every declaration ends with one.",
	},
	{
		Code: CodeExpectedType,
		Help: "write a type name or a record",
		Body: "A type expression was required and something else appeared.\n\nA type is a built-in name, a declared name, a record in braces, or one of those with [] or ? appended.",
	},
	{
		Code: CodeExpectedName,
		Help: "write an identifier",
		Body: "An identifier was required and something else appeared.\n\nBlock, service, operation, field, and route names are identifiers. A string or a punctuation mark cannot stand in for one.",
	},
	{
		Code: CodeDuplicateBlock,
		Help: "merge the two blocks into one",
		Body: "The same block keyword appears more than once in one file. Each role file declares its block once.\n\nMerge the contents of both blocks into the first one.",
	},
	{
		Code: CodeEmptyFile,
		Help: "declare the block this file owns",
		Body: "The file declares no block, so it contributes nothing to the project.\n\nA role file must declare its block, such as services { } or project { }. Delete the file if it is not needed.",
	},
	{
		Code: CodeInvalidName,
		Help: "use the case this position requires",
		Body: "An identifier does not use the case its position requires.\n\nType names are UpperCamelCase. Services, operations, fields, routes, and union tags are lower_snake_case.",
	},
	{
		Code: CodeDuplicateName,
		Help: "rename one of the declarations",
		Body: "Two declarations in the same scope share a name.\n\nThe second declaration cannot be addressed, so the project has no single meaning for that name.",
	},
	{
		Code: CodeReservedName,
		Help: "choose a name the generated code does not use",
		Body: "The name is one the generated code reserves for itself.\n\nUsing it would collide with a generated field, method, or type, so the generator cannot keep both.",
	},
	{
		Code:  CodeUnknownType,
		Help:  "declare the type in types { } or use a built-in",
		Notes: []string{"built-in types are id, string, and timestamp"},
		Body:  "A type name appears in a field, a list element, or a map value, but no types block declares it and it is not a built-in.\n\nBuilt-in types are id, string, and timestamp.",
	},
	{
		Code: CodeCyclicType,
		Help: "break the cycle by declaring one side inline",
		Body: "A named type refers to itself, directly or through other named types.\n\nA record that contains itself has no finite value, so the generated code cannot represent it.",
	},
	{
		Code: CodeInvalidModifier,
		Help: "remove the modifier or move it to a place that allows it",
		Body: "A modifier such as [] or ? is applied where the grammar does not allow it.\n\nA list element cannot itself be optional, and an optional value cannot be the element of a list.",
	},
	{
		Code: CodeInvalidMapKey,
		Help: "use string as the key type",
		Body: "A map declares a key type other than string.\n\nJSON object keys are strings, so any other key type has no wire form.",
	},
	{
		Code: CodePageNotOutput,
		Help: "use Page only as an operation output",
		Body: "Page appears somewhere other than an operation output.\n\nPage carries a next_cursor, so it is only meaningful as the value an operation returns.",
	},
	{
		Code: CodeUnionVariant,
		Help: "declare at least two variants",
		Body: "A union declares fewer than two variants.\n\nA single variant has no alternative to discriminate, so it is just a record with an extra tag.",
	},
	{
		Code: CodeUnionReserved,
		Help: "rename the field that collides with the discriminator",
		Body: "A union variant declares a field with the same name as the discriminator.\n\nThe discriminator names the variant, so the two values would occupy the same wire key.",
	},
	{
		Code: CodeEmptyRecord,
		Help: "declare at least one field or use a built-in type",
		Body: "An empty record appears where at least one field is required.\n\nA record with no fields carries no information and cannot be told apart from a missing value.",
	},
	{
		Code: CodeInvalidMethod,
		Help: "use GET, POST, PUT, PATCH, DELETE, HEAD, or OPTIONS",
		Body: "An operation or route names a method outside the allowed set.\n\nThe method is part of the address, so an unknown one has no wire meaning.",
	},
	{
		Code: CodeInvalidPath,
		Help: "start the path with / and keep it free of a query string",
		Body: "An address breaks the path rules.\n\nA path starts with /, contains no query string or fragment, and uses {name} for each placeholder.",
	},
	{
		Code: CodePathParameter,
		Help: "declare an input with the same name as the placeholder",
		Body: "A path placeholder does not match an input field.\n\nEvery {name} in the address must be an input of the operation, because the router fills it from the request.",
	},
	{
		Code: CodeRouteOverlap,
		Help: "change one address so the two cannot match the same request",
		Body: "Two addresses would match the same request.\n\nThe router would have to choose between them, and a client could not tell which one it reached.",
	},
	{
		Code: CodeReservedService,
		Help: "choose a service name the platform does not own",
		Body: "A service uses a name the platform owns, such as auth.\n\nPlatform operations already occupy that name, so a declaration would shadow them.",
	},
	{
		Code: CodeConvertUnsupported,
		Help: "write the declaration in the form the other surface accepts",
		Body: "A declaration has no equivalent in the other surface.\n\nThe conversion refuses instead of narrowing the declaration to something that compiles, so the project keeps the meaning the author wrote.",
	},
	{
		Code: CodeConvertAmbiguous,
		Help: "split the declaration so each part has one .chik form",
		Body: "A YAML declaration maps to more than one .chik form.\n\nConverting it would mean choosing an interpretation the author did not write, so the conversion refuses.",
	},
	{
		Code:  CodeOutsideBeta,
		Help:  "use the alpha form of this declaration or wait for the release that adds it",
		Notes: []string{"the alpha covers exactly what chik/*.yml declares today"},
		Body:  "The grammar defines this construct, but the alpha does not accept it.\n\nThe alpha ships the language for the declarations YAML already expresses. A later release adds routes, custom addresses, unions, and Page, together with their YAML form. The extensions block is retired: its one setting moved to project.capabilities.",
	},
	{
		Code: CodeBelowUSDCardMinimum,
		Help: "use at least 0.50 usd for card payments",
		Body: "The declared amount is below the minimum for USD card payments. Changing it does not enable unreleased operation prices.",
	},
}

// LookupExplanation returns the published description of a number.
func LookupExplanation(code string) (Explanation, bool) {
	for _, explanation := range explanations {
		if explanation.Code == code {
			return explanation, true
		}
	}
	return Explanation{}, false
}

// Explanations returns every published number in number order.
func Explanations() []Explanation {
	return explanations
}

// explanations joins the published table with this implementation's wording.
// The table decides which numbers exist, so a number added to the
// specification appears here before this tool learns to produce it.
var explanations = buildExplanations(publishedCodes, wordings)

var codeRow = regexp.MustCompile("^[|] [`](CHK[0-9]{4})[`] [|] ([a-z]+) [|] ([^|]+) [|] ([^|]+) [|]")

func buildExplanations(table string, text []wording) []Explanation {
	byCode := map[string]wording{}
	for _, entry := range text {
		byCode[entry.Code] = entry
	}
	var result []Explanation
	for _, line := range strings.Split(table, "\n") {
		match := codeRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		explanation := Explanation{Code: match[1], Group: match[2], Summary: strings.TrimSpace(match[3])}
		if entry, ok := byCode[explanation.Code]; ok {
			explanation.Help = entry.Help
			explanation.Notes = entry.Notes
			explanation.Body = entry.Body
			delete(byCode, explanation.Code)
		} else {
			explanation.Body = strings.TrimSpace(match[4])
		}
		result = append(result, explanation)
	}
	return result
}
