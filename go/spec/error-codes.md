# Error codes

Every diagnostic carries one of these numbers. A number is assigned once and
never reused, so a message can change without an old report changing meaning.
`chik explain <number>` prints the explanation for one.

Comparison in the conformance corpus uses the number, the declaration path,
and the range. Wording is not compared; see [error presentation](error-presentation.md).

| Number | Group | Summary | Meaning |
| --- | --- | --- | --- |
| `CHK0001` | lexical | unexpected character | A character that cannot start a token. |
| `CHK0002` | lexical | unterminated string | A string literal ends at end of line or file. |
| `CHK0003` | lexical | invalid escape | A backslash escape that is not a JSON escape. |
| `CHK0004` | lexical | unterminated comment | A block comment has no closing delimiter. |
| `CHK0005` | lexical | nested comment | A block comment contains another block comment. |
| `CHK0101` | syntax | expected token | A required token is missing or different. |
| `CHK0102` | syntax | expected type | A type expression was required. |
| `CHK0103` | syntax | expected name | An identifier was required. |
| `CHK0104` | syntax | duplicate block | A block appears twice in one file. |
| `CHK0105` | syntax | empty file | The file declares no block. |
| `CHK0201` | names | invalid name | An identifier is not the case its position requires. |
| `CHK0202` | names | duplicate name | Two declarations share a name in one scope. |
| `CHK0203` | names | reserved name | A name the generated code reserves. |
| `CHK0301` | types | unknown type | A named type is not declared. |
| `CHK0302` | types | cyclic type | A named type refers to itself directly or through others. |
| `CHK0303` | types | invalid modifier | A modifier applied where it is not allowed. |
| `CHK0304` | types | invalid map key | A map key that is not string. |
| `CHK0305` | types | page not output | Page used outside an operation output. |
| `CHK0306` | types | union variant count | A union with fewer than two variants. |
| `CHK0307` | types | union reserved field | A variant redeclares the discriminator field. |
| `CHK0308` | types | empty record | An empty record where at least one field is required. |
| `CHK0401` | operations | invalid method | A method outside the allowed set. |
| `CHK0402` | operations | invalid path | A path that breaks the address rules. |
| `CHK0403` | operations | path parameter | A path placeholder that does not match an input. |
| `CHK0404` | operations | route overlap | Two addresses that would match the same request. |
| `CHK0405` | operations | reserved service | A service name the platform owns. |
| `CHK0501` | conversion | unsupported | A declaration with no equivalent in the other surface. |
| `CHK0502` | conversion | ambiguous | A YAML declaration that maps to more than one form. |
| `CHK0601` | scope | outside alpha | Grammar the language defines but the alpha does not accept. |
| `CHK0602` | scope | below USD card minimum | An operation price is below the $0.50 USD card minimum. |

## Ranges

| Range | Group |
| --- | --- |
| CHK0001–CHK0099 | Lexical |
| CHK0100–CHK0199 | Syntax |
| CHK0200–CHK0299 | Names |
| CHK0300–CHK0399 | Types |
| CHK0400–CHK0499 | Operations |
| CHK0500–CHK0599 | Conversion |
| CHK0600–CHK0699 | Scope |

## Not in this release

Warnings are not part of the corpus yet. A declaration that produces only
warnings is formatted and converted normally.
