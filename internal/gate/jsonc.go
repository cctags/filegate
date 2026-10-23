package gate

import (
	"bytes"
	"fmt"
)

// stripJSONComments replaces comments with spaces while retaining line endings
// and byte offsets. String contents remain untouched; encoding/json performs
// all other syntax and type validation, including rejecting trailing commas.
func stripJSONComments(input []byte) ([]byte, error) {
	output := bytes.Clone(input)
	inString := false
	for i := 0; i < len(input); i++ {
		if inString {
			switch input[i] {
			case '\\':
				i++ // Leave the escape and its following byte for encoding/json.
			case '"':
				inString = false
			}
			continue
		}
		if input[i] == '"' {
			inString = true
			continue
		}
		if input[i] != '/' || i+1 >= len(input) {
			continue
		}
		switch input[i+1] {
		case '/':
			for ; i < len(input) && input[i] != '\n' && input[i] != '\r'; i++ {
				output[i] = ' '
			}
			i-- // Process the line ending normally on the next iteration.
		case '*':
			start := i
			output[i], output[i+1] = ' ', ' '
			i += 2
			for ; i < len(input); i++ {
				if input[i] == '*' && i+1 < len(input) && input[i+1] == '/' {
					output[i], output[i+1] = ' ', ' '
					i++
					break
				}
				if input[i] != '\n' && input[i] != '\r' {
					output[i] = ' '
				}
			}
			if i >= len(input) {
				return nil, fmt.Errorf("unterminated block comment at byte %d", start+1)
			}
		}
	}
	return output, nil
}
