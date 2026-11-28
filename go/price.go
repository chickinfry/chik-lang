package chiklang

import (
	"regexp"
	"strings"
)

const operationPriceUnavailableMessage = "operation prices are not supported yet"

var (
	decimalPrice = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	currencyCode = regexp.MustCompile(`^[a-z]+$`)
)

func (p *parser) operationPrice(path string) *Node {
	keyword := p.next()
	node := &Node{Kind: NodeOperationPrice, Path: path, NameRange: nodeRange(keyword, keyword)}
	for len(node.Tokens) < 2 && p.at().Line == keyword.Line && p.at().Kind != EOF &&
		!p.atPunct("}") && !p.atPunct(";") {
		node.Tokens = append(node.Tokens, p.next())
	}
	node.Range = nodeRange(keyword, p.tokens[p.pos-1])
	p.diags = append(p.diags, operationPriceUnavailable(node, ""))

	amount := keyword
	if len(node.Tokens) != 0 {
		amount = node.Tokens[0]
	}
	if amount.Kind != Number || !decimalPrice.MatchString(amount.Text) || strings.Trim(amount.Text, "0.") == "" {
		p.diags = append(p.diags, Diagnostic{
			Code: CodeExpectedToken, Severity: "error", Path: path + ".amount",
			Range: nodeRange(amount, amount), Message: "price amount must be a positive decimal number",
		})
		return node
	}

	currency := amount
	if len(node.Tokens) == 2 {
		currency = node.Tokens[1]
	}
	if currency.Kind != Ident || !currencyCode.MatchString(currency.Text) {
		p.diags = append(p.diags, Diagnostic{
			Code: CodeExpectedName, Severity: "error", Path: path + ".currency",
			Range: nodeRange(currency, currency), Message: "price must specify a lowercase currency code",
		})
		return node
	}

	// Compare the decimal spelling directly so a price just below 0.50 stays below it.
	whole, fraction, _ := strings.Cut(amount.Text, ".")
	if currency.Text == "usd" && strings.TrimLeft(whole, "0") == "" && fraction[0] < '5' {
		p.diags = append(p.diags, Diagnostic{
			Code: CodeBelowUSDCardMinimum, Severity: "warning", Path: path + ".amount",
			Range: nodeRange(amount, amount), Message: "USD card payments require at least $0.50",
		})
	}
	return node
}

func operationPriceUnavailable(node *Node, file string) Diagnostic {
	return Diagnostic{
		Code: CodeOutsideBeta, Severity: "error", File: file, Path: node.Path,
		Range: node.NameRange, Message: operationPriceUnavailableMessage,
	}
}
