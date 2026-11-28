// Package chiklang parses, formats, and edits .chik declarations.
package chiklang

// NodeKind names a concrete syntax tree node. Kinds are additive: a new
// language feature adds a kind, and an implementation that meets an unknown
// kind rejects the file instead of ignoring the node.
type NodeKind string

const (
	NodeFile            NodeKind = "File"
	NodeProjectBlock    NodeKind = "ProjectBlock"
	NodeSchemaBlock     NodeKind = "SchemaBlock"
	NodeAPIBlock        NodeKind = "APIBlock"
	NodeClientsBlock    NodeKind = "ClientsBlock"
	NodeAuthBlock       NodeKind = "AuthBlock"
	NodeExtensionsBlock NodeKind = "ExtensionsBlock"
	NodeTypesBlock      NodeKind = "TypesBlock"
	NodeServicesBlock   NodeKind = "ServicesBlock"
	NodeRoutesBlock     NodeKind = "RoutesBlock"
	NodeService         NodeKind = "Service"
	NodeOperation       NodeKind = "Operation"
	NodeOperationPrice  NodeKind = "OperationPrice"
	NodeParam           NodeKind = "Param"
	NodeResult          NodeKind = "Result"
	NodeField           NodeKind = "Field"
	NodeTypeRef         NodeKind = "TypeRef"
	NodeTypeList        NodeKind = "TypeList"
	NodeTypeNullable    NodeKind = "TypeNullable"
	NodeTypeRecord      NodeKind = "TypeRecord"
	NodeTable           NodeKind = "Table"
	NodeColumn          NodeKind = "Column"
	NodeEntry           NodeKind = "Entry"
	NodeError           NodeKind = "Error"
)

// Node is one concrete syntax tree node. A node has either children or tokens,
// never both. Serializing a parsed tree returns the original bytes.
type Node struct {
	Kind  NodeKind
	Range Range
	Name  string
	// Path is the declaration path a diagnostic uses, such as
	// "types.Post.author". The parser sets it where the position is known.
	Path string
	// Optional marks an input field the declaration lets a caller omit.
	Optional bool
	// NameRange covers the block keyword, which is the range a duplicate-block
	// diagnostic points at.
	NameRange Range
	Children  []*Node
	Tokens    []Token
	// Leading and Trailing hold the trivia this node owns. The split follows
	// the attachment rules in spec/cst.md, which make formatting idempotent.
	Leading  []Trivia
	Trailing []Trivia
}

// File is a parsed declaration file.
type File struct {
	Path        string
	Root        *Node
	Diagnostics []Diagnostic
	Source      string
	Tokens      []Token
	Trivia      []Trivia
}
