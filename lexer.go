package directivefile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// utf8BOM is the UTF-8 byte order mark. It is metadata, not file content.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// tokenize lexes input into a sequence of tokens.
// Comments are discarded during lexing.
// filename is used as the source filename in token positions.
// The input MUST be UTF-8 encoded; a leading UTF-8 BOM is ignored.
func tokenize(input []byte, filename string) ([]Token, error) {
	input = bytes.TrimPrefix(input, utf8BOM)
	if !utf8.Valid(input) {
		return nil, fmt.Errorf("%s: invalid UTF-8", filename)
	}

	l, err := newLexer(input, filename)
	if err != nil {
		return nil, err
	}
	var tokens []Token
	for {
		token, found, err := l.nextToken()
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		tokens = append(tokens, token)
	}

	// If the file's last directive is not terminated by a NEWLINE, append one
	// automatically so the parser never needs an end-of-file special case.
	if len(input) > 0 && (len(tokens) == 0 || tokens[len(tokens)-1].Type != NewLine) {
		tokens = append(tokens, Token{
			Type: NewLine,
			Position: Position{
				Filename: filename,
				Line:     l.nextRuneLine,
				Column:   l.nextRuneColumn,
			},
		})
	}
	return tokens, nil
}

// lexer reads tokens from a Reader, one token at a time.
type lexer struct {
	reader   *bufio.Reader
	filename string // source file, copied into every Token.Pos.File

	nextRuneLine   int
	nextRuneColumn int

	prevToken Token
}

// newLexer returns a lexer over input, with any leading BOM skipped.
// Empty input is valid and leaves the lexer at EOF.
func newLexer(input []byte, filename string) (*lexer, error) {
	l := &lexer{
		reader:         bufio.NewReader(bytes.NewReader(input)),
		filename:       filename,
		nextRuneLine:   1,
		nextRuneColumn: 1,
	}

	firstCh, _, err := l.reader.ReadRune()
	if err != nil {
		if err == io.EOF {
			return l, nil // empty input
		}
		return nil, err
	}
	if firstCh != 0xFEFF {
		if err := l.reader.UnreadRune(); err != nil {
			return nil, err
		}
	}

	return l, nil
}

func (l *lexer) nextToken() (Token, bool, error) {
Start:
	pos := Position{
		Filename: l.filename,
		Line:     l.nextRuneLine,
		Column:   l.nextRuneColumn,
	}
	// ch may be whitespace, LF, #, or the first rune of a token.
	ch, err := l.nextRune()
	if err != nil {
		if err == io.EOF {
			return Token{}, false, nil
		}
		return Token{}, false, err
	}
	// Skip token separators and comments until the next token starts.
	for {
		if IsTokenSeparator(ch) {
			pos.Line = l.nextRuneLine
			pos.Column = l.nextRuneColumn
			ch, err = l.nextRune()
			if err != nil {
				if err == io.EOF {
					return Token{}, false, nil
				}
				return Token{}, false, err
			}
			continue
		}

		if ch == LF {
			tok := Token{
				Type:     NewLine,
				Position: pos,
			}
			l.prevToken = tok
			return tok, true, nil
		}
		// ch is the first rune of a token, a comment, or a possible line continuation.
		if ch == HashTag {
			if err := l.scanComment(); err != nil {
				return Token{}, false, err
			}
			pos.Line = l.nextRuneLine
			pos.Column = l.nextRuneColumn
			ch, err = l.nextRune()
			if err != nil {
				if err == io.EOF {
					return Token{}, false, nil
				}
				return Token{}, false, err
			}
			continue
		}

		break
	}

	// 先处理掉续行符
	if ch == BackSlash {
		// 判断是否与前一个 token 同行
		if l.prevToken.Position.Line != pos.Line {
			goto FindToken
		}
		// 判断是否是独立的 Token
		next, err := l.peekRune()
		if err != nil {
			if err == io.EOF { // 直接返回 UnquotedToken
				tok := Token{
					Type:     UnquotedToken,
					Text:     "\\",
					Position: pos,
				}
				l.prevToken = tok
				return tok, true, nil
			}
			return Token{}, false, err
		}
		if !unicode.IsSpace(next) {
			goto FindToken
		}

		ok, err := l.scanLineContinuation()
		if err != nil {
			if err == io.EOF {
				tok := Token{
					Type:     UnquotedToken,
					Text:     "\\",
					Position: pos,
				}
				l.prevToken = tok
				return tok, true, nil
			}
			return Token{}, false, err
		}

		if ok {
			goto Start
		}

		tok := Token{
			Type:     UnquotedToken,
			Text:     "\\",
			Position: pos,
		}
		l.prevToken = tok
		return tok, true, nil
	}

FindToken:
	if ch == DoubleQuote {
		tok, err := l.scanDoubleQuoted(pos)
		if err != nil {
			return Token{}, false, err
		}
		l.prevToken = tok
		return tok, true, nil
	}

	if ch == BacktickQuote {
		tok, err := l.scanBacktickQuoted(pos)
		if err != nil {
			return Token{}, false, err
		}
		l.prevToken = tok
		return tok, true, nil
	}

	tok, err := l.scanUnquoted(pos, ch)
	if err != nil {
		return Token{}, false, err
	}
	l.prevToken = tok
	return tok, true, nil
}

func (l *lexer) nextRune() (rune, error) {
	ch, _, err := l.reader.ReadRune()
	if err != nil {
		return 0, err
	}

	if ch == LF {
		l.nextRuneLine++
		l.nextRuneColumn = 1
	} else {
		l.nextRuneColumn++
	}
	return ch, nil
}

func (l *lexer) peekRune() (rune, error) {
	ch, _, err := l.reader.ReadRune()
	if err != nil {
		return 0, err
	}
	if err := l.reader.UnreadRune(); err != nil {
		return 0, err
	}
	return ch, nil
}

// 注释结束条件
// 1 遇到文件末尾
// 2 遇到换行符
func (l *lexer) scanComment() error {
	for {
		ch, err := l.peekRune()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if ch == LF {
			return nil
		}
		_, err = l.nextRune()
		if err != nil {
			return err
		}
	}
}

// scanUnquoted reads the rest of a bare token.
// pos 是 first 的位置，也是将要返回的 Token 的位置
func (l *lexer) scanUnquoted(pos Position, first rune) (Token, error) {
	text := []rune{first}

	for {
		ch, err := l.peekRune()
		if err != nil {
			if err == io.EOF {
				return Token{
					Type:     UnquotedToken,
					Text:     string(text),
					Position: pos,
				}, nil
			}
			return Token{}, err
		}

		if IsTokenSeparator(ch) || ch == LF {
			return Token{
				Type:     UnquotedToken,
				Text:     string(text),
				Position: pos,
			}, nil
		}

		_, err = l.nextRune()
		if err != nil {
			return Token{}, err
		}

		text = append(text, ch)
	}
}

func (l *lexer) scanDoubleQuoted(pos Position) (Token, error) {
	var text []rune
	var needEscape bool
	for {
		ch, err := l.nextRune()
		if err != nil {
			if err == io.EOF {
				return Token{}, fmt.Errorf(
					"%s: unterminated double-quoted string", pos)
			}
			return Token{}, err
		}

		if ch == LF {
			return Token{}, fmt.Errorf(
				"%s: unterminated double-quoted string", pos)
		}

		if needEscape {
			needEscape = false
			switch ch {
			case 'a':
				text = append(text, '\a')
			case 'b':
				text = append(text, '\b')
			case 'f':
				text = append(text, '\f')
			case 'n':
				text = append(text, '\n')
			case 'r':
				text = append(text, '\r')
			case 't':
				text = append(text, '\t')
			case 'v':
				text = append(text, '\v')
			case '\\':
				text = append(text, '\\')
			case '"':
				text = append(text, '"')
			case 'u':
				ch, err := l.scanUnicodeEscape(pos, 4)
				if err != nil {
					if err == io.EOF {
						return Token{}, fmt.Errorf(
							"%s: unterminated double-quoted string", pos)
					}
					return Token{}, err
				}
				text = append(text, ch)
			case 'U':
				ch, err := l.scanUnicodeEscape(pos, 8)
				if err != nil {
					if err == io.EOF {
						return Token{}, fmt.Errorf(
							"%s: unterminated double-quoted string", pos)
					}
					return Token{}, err
				}
				text = append(text, ch)
			default:
				// Unknown escapes are preserved literally.
				text = append(text, '\\', ch)
			}
			continue
		}

		if ch == BackSlash {
			needEscape = true
			continue

		}

		if ch == DoubleQuote {
			next, err := l.peekRune()
			if err != nil && err != io.EOF {
				return Token{}, err
			}
			if err == nil && !IsTokenSeparator(next) && next != LF {
				return Token{}, fmt.Errorf(
					"%s: unexpected character after double-quoted string", pos)
			}

			tok := Token{
				Type:     DoubleQuotedToken,
				Text:     string(text),
				Position: pos, // 起始位置
			}
			return tok, nil
		}

		text = append(text, ch)
	}
}

func (l *lexer) scanUnicodeEscape(pos Position, n int) (rune, error) {
	var value rune
	for i := 0; i < n; i++ {
		ch, err := l.nextRune()
		if err != nil {
			return 0, err
		}

		value <<= 4

		switch {
		case '0' <= ch && ch <= '9':
			value += ch - '0'
		case 'a' <= ch && ch <= 'f':
			value += ch - 'a' + 10
		case 'A' <= ch && ch <= 'F':
			value += ch - 'A' + 10
		default:
			return 0, fmt.Errorf("%s: has invalid Unicode escape sequence", pos)
		}
	}

	if value > 0x10FFFF || 0xD800 <= value && value <= 0xDFFF {
		return 0, fmt.Errorf("%s: has invalid Unicode escape sequence", pos)
	}

	return value, nil
}

// ok=false, err=nil  → 不是续行，\ 是普通 token
// ok=true,  err=nil  → 是合法续行
// ok=false, err!=nil → 其他错误
func (l *lexer) scanLineContinuation() (bool, error) {
	// Consume Token Separator after the backslash.
	for {
		ch, err := l.peekRune()
		if err != nil {
			return false, err
		}

		if !IsTokenSeparator(ch) {
			if ch != LF {
				return false, nil // 不是续行符 是普通的 Token
			}
			_, err = l.nextRune() // 消耗 LF，不产 NewLine
			return err == nil, err
		}

		if _, err := l.nextRune(); err != nil {
			return false, err
		}
	}
}

// 反引号位于一个 Token的开头，由调用者保证
// `后会一直扫描 rune 直到遇到反引号停止
//   - peek 下一个 rune 必须是代码分隔符，LF，文件结束io.EOF
//
// `...` 内不会执行任何义
func (l *lexer) scanBacktickQuoted(pos Position) (Token, error) {
	text := make([]rune, 0)

	for {
		ch, err := l.nextRune()
		if err != nil {
			if err == io.EOF {
				return Token{}, fmt.Errorf(
					"%s: unterminated backtick-quoted string", pos)
			}
			return Token{}, err
		}

		if ch != BacktickQuote {
			text = append(text, ch)
			continue
		}

		// The closing backtick must terminate the token.
		next, err := l.peekRune()
		if err != nil {
			if err == io.EOF {
				return Token{
					Type:     BacktickQuotedToken,
					Text:     string(text),
					Position: pos,
				}, nil
			}
			return Token{}, err
		}

		if next != LF && !IsTokenSeparator(next) {
			return Token{}, fmt.Errorf(
				"%s: unexpected character after backtick string", pos)
		}

		return Token{
			Type:     BacktickQuotedToken,
			Text:     string(text),
			Position: pos,
		}, nil
	}
}
