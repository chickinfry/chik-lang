// Package chiklang parses the .chik declaration language into a concrete
// syntax tree that serializes back to the exact source bytes.
package chiklang

// Kind is the lexical class of a token.
type Kind int

const (
	// EOF ends the token stream.
	EOF Kind = iota
	// Ident is an identifier or keyword. Position decides meaning.
	Ident
	// String is a double-quoted string literal with escapes already decoded.
	String
	// Punct is one of the punctuation characters the grammar uses.
	Punct
	// Number is an integer or decimal. It is a configuration value, not a
	// declaration type.
	Number
)

// Token is one lexical unit with the byte span it occupies in the source.
type Token struct {
	Kind Kind
	// Text is the decoded value for a string token and the source spelling for
	// every other kind.
	Text string
	// Raw is the exact source spelling. A roundtrip uses it so an escape such as
	// \u00e9 is preserved rather than normalized to the character it encodes.
	Raw    string
	Start  int
	End    int
	Line   int
	Column int
}

// Pos is a one-based line and column. Column counts Unicode code points.
type Pos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// width counts the code points the token occupies in the source. A string
// token's Text holds the decoded value, which is shorter than its spelling, so
// a range that used Text would end inside the literal.
func (t Token) width() int {
	if t.Raw != "" {
		return len([]rune(t.Raw))
	}
	return len([]rune(t.Text))
}

// Range is a half-open span: Start is included, End is not.
type Range struct {
	Start Pos `json:"start"`
	End   Pos `json:"end"`
}

// Diagnostic is one problem found in a declaration. Code is stable and is the
// only part other implementations must match; Message is for humans.
type Diagnostic struct {
	Code     string
	Severity string
	// File is the declaration file the diagnostic belongs to, as a path
	// relative to the project root.
	File    string
	Path    string
	Range   Range
	Message string
	Help    string
	Notes   []string
	// Suggestion is the corrected line, set only where the fix is unambiguous.
	Suggestion *Suggestion
}

// Suggestion is a corrected line a reader can paste over the failing one.
type Suggestion struct {
	Text string `json:"text"`
}

// Error codes from the published list. A code is assigned once and never
// reused, so a message can change without an old report changing meaning.
const (
	CodeUnexpectedCharacter = "CHK0001"
	CodeUnterminatedString  = "CHK0002"
	CodeInvalidEscape       = "CHK0003"
	CodeUnterminatedComment = "CHK0004"
	CodeNestedComment       = "CHK0005"

	CodeExpectedToken  = "CHK0101"
	CodeExpectedType   = "CHK0102"
	CodeExpectedName   = "CHK0103"
	CodeDuplicateBlock = "CHK0104"
	CodeEmptyFile      = "CHK0105"

	CodeInvalidName   = "CHK0201"
	CodeDuplicateName = "CHK0202"
	CodeReservedName  = "CHK0203"

	CodeUnknownType     = "CHK0301"
	CodeCyclicType      = "CHK0302"
	CodeInvalidModifier = "CHK0303"
	CodeInvalidMapKey   = "CHK0304"
	CodePageNotOutput   = "CHK0305"
	CodeUnionVariant    = "CHK0306"
	CodeUnionReserved   = "CHK0307"
	CodeEmptyRecord     = "CHK0308"

	CodeInvalidMethod   = "CHK0401"
	CodeInvalidPath     = "CHK0402"
	CodePathParameter   = "CHK0403"
	CodeRouteOverlap    = "CHK0404"
	CodeReservedService = "CHK0405"

	CodeConvertUnsupported = "CHK0501"
	CodeConvertAmbiguous   = "CHK0502"

	// CodeOutsideBeta reports grammar the language defines but the beta does
	// not accept. A beta file must not depend on a construct the next release
	// could change without notice.
	CodeOutsideBeta         = "CHK0601"
	CodeBelowUSDCardMinimum = "CHK0602"
)
