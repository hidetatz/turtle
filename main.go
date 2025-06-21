package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

/*
 * debug setting
 */

var _debuglevel int

func init() {
	d := os.Getenv("TURTLE_DEBUG")
	if d == "" {
		_debuglevel = 0
		return
	}

	i, err := strconv.ParseInt(d, 10, 64)
	if err != nil {
		panic("TURTLE_DEBUG must be an number")
	}

	_debuglevel = int(i)
}

/*
 * mode
 */

type mode int

const (
	normal mode = iota + 1
	insert
	command
)

func (m mode) String() string {
	switch m {
	case normal:
		return "NOR"
	case insert:
		return "INS"
	case command:
		return "CMD"
	default:
		panic("unknown mode")
	}
}

/*
 * character
 */

type character struct {
	r     rune
	tab   bool // true if the character represents Tab.
	nl    bool // true if the character represents new line.
	width int
	disp  string // string representation to display on screen
}

func newcharacter(r rune) *character {
	// Raw Tab changes its size dynamically and it's hard to properly display, so
	// Tab is treated as 4 spaces.
	if r == '\t' {
		return &character{tab: true, width: 4, disp: "    "}
	}

	// In turtle newline is rendered as a single space.
	if r == '\n' {
		return &character{nl: true, width: 1, disp: " "}
	}

	if fullwidth(r) {
		return &character{r: r, width: 2, disp: string(r)}
	}

	return &character{r: r, width: 1, disp: string(r)}
}

func (c *character) copy() *character {
	return &character{c.r, c.tab, c.nl, c.width, c.disp}
}

func (c *character) isspace() bool {
	return c.r == ' ' || c.tab
}

func (c *character) equal(c2 *character) bool {
	if c.tab {
		return c2.tab
	}

	if c.nl {
		return c2.nl
	}

	return c.r == c2.r
}

func (c *character) String() string {
	return c.disp
}

/*
 * line
 */

type line struct {
	buffer []*character
}

func newcommandline() *line {
	return newemptyline()
}

func newemptyline() *line {
	return &line{buffer: []*character{newcharacter('\n')}}
}

func newline(s string) *line {
	runes := []rune(s)
	buff := make([]*character, len(runes))
	for i := range runes {
		buff[i] = newcharacter(runes[i])
	}
	buff = append(buff, newcharacter('\n'))
	return &line{buffer: buff}
}

func (l *line) String() string {
	// todo: can be cached
	sb := strings.Builder{}
	for _, c := range l.buffer {
		sb.WriteString(c.String())
	}
	return sb.String()
}

func (l *line) charidx(cursor, offset int) int {
	x := -offset
	for i, c := range l.buffer {
		x += c.width
		if x >= cursor+1 {
			return i
		}
	}

	panic("must not happen")
}

func (l *line) length() int {
	return len(l.buffer)
}

func (l *line) widthto(idx int) int {
	x := 0
	for i := range idx {
		x += l.buffer[i].width
	}
	return x
}

func (l *line) width() int {
	// todo: can be cached
	return l.widthto(l.length())
}

func (l *line) replacech(ch *character, at int) {
	l.buffer[at] = ch
}

func (l *line) inschars(chars []*character, at int) {
	for i := range chars {
		l.buffer = slices.Insert(l.buffer, at+i, chars[i])
	}
}

func (l *line) delnl() {
	l.delchar(l.length() - 1)
}

func (l *line) delchar(at int) {
	l.buffer = slices.Delete(l.buffer, at, at+1)
}

func (l *line) equal(s string) bool {
	rs := []rune(s)
	if len(l.buffer) != len(rs)+1 {
		return false
	}

	for i := range rs {
		if l.buffer[i].tab {
			if rs[i] != '\t' {
				return false
			}
		}

		if l.buffer[i].r != rs[i] {
			return false
		}
	}

	return true
}

func (l *line) copy() *line {
	copy := &line{buffer: make([]*character, len(l.buffer))}
	for i := range l.buffer {
		copy.buffer[i] = l.buffer[i].copy()
	}
	return copy
}

func (l *line) clear() {
	l.buffer = []*character{newcharacter('\n')}
}

func (l *line) empty() bool {
	return l.length() == 1 && l.buffer[0].nl
}

func (l *line) hasprefix(prefix string) bool {
	return strings.HasPrefix(l.String(), prefix)
}

func (l *line) trimprefix(prefix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(l.String(), prefix), " ")
}

func (l *line) substring(start, end int) string {
	s := ""
	for i := start; i < end; i++ {
		s += l.buffer[i].disp
	}
	return s
}

func (l *line) cutandcolorize(from, width int, colors []int, inverts []int) string {
	colorize := func(s string, color int) string {
		if color == -1 {
			return s
		}
		return fmt.Sprintf("\x1b[38;5;%dm%v\x1b[0m", int(color), s)
	}

	invert := func(s string) string {
		return fmt.Sprintf("\x1b[7m%v\x1b[27m", s)
	}

	var runes []rune
	var _colors []int
	var widths []int
	var _inverts []int

	for i := range l.buffer {
		switch {
		case l.buffer[i].tab:
			runes = append(runes, ' ', ' ', ' ', ' ')
			if len(colors) != 0 {
				_colors = append(_colors, colors[i], colors[i], colors[i], colors[i])
			}
			widths = append(widths, 1, 1, 1, 1)

			if slices.Contains(inverts, i) {
				length := len(runes)
				_inverts = append(_inverts, length-4, length-3, length-2, length-1)
			}

		case l.buffer[i].nl:
			runes = append(runes, ' ')
			widths = append(widths, 1)
			_colors = append(_colors, -1)
			if slices.Contains(inverts, i) {
				length := len(runes)
				_inverts = append(_inverts, length-1)
			}

		default:
			runes = append(runes, l.buffer[i].r)
			if len(colors) != 0 {
				_colors = append(_colors, colors[i])
			}
			widths = append(widths, l.buffer[i].width)

			if slices.Contains(inverts, i) {
				length := len(runes)
				_inverts = append(_inverts, length-1)
			}
		}
	}

	var str string
	curwidth := 0
	for i := range len(runes) {
		if from+width < curwidth {
			break
		}

		curwidth += widths[i]
		if curwidth <= from {
			continue
		}

		if slices.Contains(_inverts, i) {
			str += invert(string(runes[i]))
			continue
		}

		if len(colors) == 0 {
			str += string(runes[i])
			continue
		}

		str += colorize(string(runes[i]), _colors[i])
	}

	return str
}

/*
 * highlighter
 */

type highlighter interface {
	highlightline(l *line, prevlineattr *lineattribute) *lineattribute
}

// color command:
// for i in {0..256} ; do printf "\e[38;5;${i}m%3d \e[0m" $i ; [ $((i % 16)) -eq 0 ] && echo ; done
type theme struct {
	colorident           int
	colorkeyword         int
	coloroperator        int
	colorsymbol          int
	colorstring          int
	colormultilinestring int
	colornumber          int
	colorlinecomment     int
	colorblockcomment    int
}

var (
	theme_doraemon = &theme{
		colorident:           32,
		colorkeyword:         -1,
		coloroperator:        220,
		colorsymbol:          220,
		colorstring:          160,
		colormultilinestring: 160,
		colornumber:          202,
		colorlinecomment:     240,
		colorblockcomment:    240,
	}

	theme_nobita = &theme{
		colorident:           11,
		colorkeyword:         -1,
		coloroperator:        27,
		colorsymbol:          27,
		colorstring:          180,
		colormultilinestring: 180,
		colornumber:          38,
		colorlinecomment:     240,
		colorblockcomment:    240,
	}

	theme_shizuka = &theme{
		colorident:           176,
		colorkeyword:         217,
		coloroperator:        125,
		colorsymbol:          125,
		colorstring:          125,
		colormultilinestring: 125,
		colornumber:          11,
		colorlinecomment:     240,
		colorblockcomment:    240,
	}

	theme_suneo = &theme{
		colorident:           34,
		colorkeyword:         217,
		coloroperator:        130,
		colorsymbol:          130,
		colorstring:          220,
		colormultilinestring: 220,
		colornumber:          -1,
		colorlinecomment:     240,
		colorblockcomment:    240,
	}

	theme_gian = &theme{
		colorident:           208,
		colorkeyword:         186,
		coloroperator:        21,
		colorsymbol:          21,
		colorstring:          216,
		colormultilinestring: 216,
		colornumber:          172,
		colorlinecomment:     240,
		colorblockcomment:    240,
	}
)

type nophighlighter struct{}

func (h nophighlighter) highlightline(l *line, _ *lineattribute) *lineattribute {
	return &lineattribute{colors: []int{}}
}

type clikelangbasichighlighter struct {
	linetokenizer *clikelanglinetokenizer
	theme         *theme
}

func newgolanghighlighter(theme *theme) *clikelangbasichighlighter {
	return &clikelangbasichighlighter{
		theme: theme,
		linetokenizer: &clikelanglinetokenizer{
			linecommentstart:      []rune{'/', '/'},
			blockcommentstart:     []rune{'/', '*'},
			blockcommentend:       []rune{'*', '/'},
			stringstarts:          [][]rune{{'"'}, {'\''}},
			stringends:            [][]rune{{'"'}, {'\''}},
			multilinestringstarts: [][]rune{{'`'}},
			multilinestringends:   [][]rune{{'`'}},
			keywords: []string{
				"append", "copy", "delete", "len", "cap", "make", "max", "min", "new", "complex", "real", "imag", "clear", "close", "panic", "recover", "print", "println",
				"package", "import", "func", "defer", "return", "for", "range", "for", "if", "else", "var", "const", "switch", "case", "goto", "fallthrough", "default",
				"type", "struct", "interface", "map", "select", "go", "chan", "iota", "nil", "true", "false",
				"bool", "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "float32", "float64", "complex64", "complex128",
				"string", "int", "uint", "uintptr", "byte", "rune", "any", "error", "comparable",
				"archive", "tar", "zip", "bufio", "builtin", "bytes", "cmp", "compress", "bzip2", "flate", "gzip", "lzw", "zlib", "container", "heap",
				"list", "ring", "context", "crypto", "aes", "cipher", "des", "dsa", "ecdh", "ecdsa", "ed25519", "elliptic", "fips140", "hkdf", "hmac", "md5", "mlkem", "pbkdf2", "rand", "rc4",
				"rsa", "sha1", "sha256", "sha3", "sha512", "subtle", "tls", "x509", "pkix", "database", "sql", "driver", "debug", "buildinfo", "dwarf", "elf", "gosym", "macho", "pe", "plan9obj", "embed",
				"encoding", "ascii85", "asn1", "base32", "base64", "binary", "csv", "gob", "hex", "json", "pem", "xml", "errors", "expvar", "flag", "fmt", "go", "ast", "build", "constraint", "constant",
				"doc", "comment", "format", "importer", "parser", "printer", "scanner", "token", "types", "version", "hash", "adler32", "crc32", "crc64", "fnv", "maphash", "html", "template", "image", "color",
				"palette", "draw", "gif", "jpeg", "png", "index", "suffixarray", "io", "fs", "ioutil", "iter", "log", "slog", "syslog", "maps", "math", "big", "bits", "cmplx", "rand", "mime",
				"multipart", "quotedprintable", "net", "http", "cgi", "cookiejar", "fcgi", "httptest", "httptrace", "httputil", "pprof", "mail", "netip", "rpc", "jsonrpc", "smtp", "textproto", "url", "os", "exec",
				"signal", "user", "path", "filepath", "plugin", "reflect", "regexp", "syntax", "runtime", "cgo", "coverage", "debug", "metrics", "pprof", "race", "trace",
				"slices", "sort", "strconv", "strings", "structs", "sync", "atomic", "syscall", "js", "testing", "fstest", "iotest", "quick", "slogtest", "synctest",
				"text", "scanner", "tabwriter", "template", "parse", "time", "tzdata", "unicode", "utf16", "utf8", "unique", "unsafe", "weak",
			},
			symbols:    []string{"[", "]", "(", ")", "{", "}", ":", ";", ",", "."},
			operators:  []string{"!", "+", "-", "*", "/", "%", "&", "|", "=", "<", ">", "~"},
			operators2: []string{"++", "--", ":=", "==", "<=", ">=", "!=", "+=", "-=", "*=", "/=", "|=", "&=", "%=", "&&", "||", "<<", ">>"},
			operators3: []string{">>=", "<<=", "&^="},
		},
	}
}

func newpythonhighlighter(theme *theme) *clikelangbasichighlighter {
	return &clikelangbasichighlighter{
		theme: theme,
		linetokenizer: &clikelanglinetokenizer{
			linecommentstart:      []rune{'#'},
			stringstarts:          [][]rune{{'"'}, {'\''}, {'b', '"'}, {'f', '"'}},
			stringends:            [][]rune{{'"'}, {'\''}, {'"'}, {'"'}},
			multilinestringstarts: [][]rune{{'"', '"', '"'}, {'\'', '\'', '\''}},
			multilinestringends:   [][]rune{{'"', '"', '"'}, {'\'', '\'', '\''}},
			keywords: []string{
				"False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue", "def", "del", "elif", "else", "except", "finally", "for",
				"from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield",
				"abs", "aiter", "all", "anext", "any", "ascii", "bin", "bool", "breakpoint", "bytearray", "bytes", "callable", "chr", "classmethod", "compile", "complex",
				"delattr", "dict", "dir", "divmod", "enumerate", "eval", "exec", "filter", "float", "format", "frozenset", "getattr", "globals",
				"hasattr", "hash", "help", "hex", "id", "input", "int", "isinstance", "issubclass", "iter", "len", "list", "locals",
				"map", "max", "memoryview", "min", "next", "object", "oct", "open", "ord", "pow", "print", "property",
				"range", "repr", "reversed", "round", "set", "setattr", "slice", "sorted", "staticmethod", "str", "sum", "super", "tuple", "type", "vars", "zip", "__import__",
			},
			symbols:    []string{"[", "]", "(", ")", "{", "}", ":", ";", ",", "."},
			operators:  []string{"+", "-", "*", "/", "%", "~", "&", "|", "^", "=", "<", ">", "@"},
			operators2: []string{"**", "//", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "==", "!=", "<=", ">=", ":=", "is", "in", "or"},
			operators3: []string{"**=", "//=", ">>=", "<<=", "and", "not"},
		},
	}
}

func (h clikelangbasichighlighter) highlightline(l *line, prevlineattr *lineattribute) *lineattribute {
	tokens, curlineattr := h.linetokenizer.tokenizeline(l, prevlineattr)
	colors := make([]int, l.length())
	for _, token := range tokens {
		for i := token.start; i < token.end+1; i++ {
			switch token.typ {
			case tk_unknown, tk_whitespace, tk_nl:
				colors[i] = -1
			case tk_ident:
				colors[i] = h.theme.colorident
			case tk_keyword:
				colors[i] = h.theme.colorkeyword
			case tk_string:
				colors[i] = h.theme.colorstring
			case tk_multilinestring:
				colors[i] = h.theme.colormultilinestring
			case tk_number:
				colors[i] = h.theme.colornumber
			case tk_operator:
				colors[i] = h.theme.coloroperator
			case tk_symbol:
				colors[i] = h.theme.colorsymbol
			case tk_linecomment:
				colors[i] = h.theme.colorlinecomment
			case tk_blockcomment:
				colors[i] = h.theme.colorblockcomment
			}
		}
	}

	curlineattr.colors = colors
	return curlineattr
}

/* tokenizer */

type tokentype int

const (
	tk_unknown tokentype = iota
	tk_ident
	tk_keyword
	tk_string
	tk_multilinestring
	tk_number
	tk_operator
	tk_symbol
	tk_linecomment
	tk_blockcomment
	tk_whitespace
	tk_nl
)

func (t tokentype) String() string {
	switch t {
	case tk_unknown:
		return "unknown"
	case tk_ident:
		return "ident"
	case tk_keyword:
		return "keyword"
	case tk_string:
		return "string"
	case tk_multilinestring:
		return "multilinestring"
	case tk_number:
		return "number"
	case tk_operator:
		return "operator"
	case tk_symbol:
		return "symbol"
	case tk_linecomment:
		return "linecomment"
	case tk_blockcomment:
		return "blockcomment"
	case tk_whitespace:
		return "whitespace"
	case tk_nl:
		return "nl"
	default:
		panic("unknown tokentype")
	}
}

type token struct {
	typ        tokentype
	start, end int

	blockcommentterminated bool

	multilinestrterminated bool
	multilinestrstart      []rune
	multilinestrend        []rune
}

func (t *token) String() string {
	switch t.typ {
	case tk_blockcomment:
		return fmt.Sprintf("{%v (%v-%v) (terminated: %v)}", t.typ, t.start, t.end, t.blockcommentterminated)
	case tk_multilinestring:
		return fmt.Sprintf("{%v (%v-%v) (terminated: %v, quote: %v%v)}", t.typ, t.start, t.end, t.multilinestrterminated, t.multilinestrstart, t.multilinestrend)
	default:
		return fmt.Sprintf("{%v (%v-%v)}", t.typ, t.start, t.end)
	}
}

type clikelanglinetokenizer struct {
	line *line
	pos  int

	// language setting

	linecommentstart  []rune
	blockcommentstart []rune
	blockcommentend   []rune

	stringstarts          [][]rune
	stringends            [][]rune
	rawstringstarts       [][]rune
	rawstringends         [][]rune
	multilinestringstarts [][]rune
	multilinestringends   [][]rune

	keywords   []string
	symbols    []string
	operators  []string // single character
	operators2 []string // 2 characters
	operators3 []string // 3 characters
}

func (t *clikelanglinetokenizer) tokenizeline(l *line, prevlinestate *lineattribute) ([]*token, *lineattribute) {
	t.line = l
	t.pos = 0

	var tokens []*token
	inmultilinecomment, inmultilinestring := prevlinestate.inblockcomment, prevlinestate.inmultilinestr
	multilinestrstart, multilinestrend := prevlinestate.multilinestrstart, prevlinestate.multilinestrend
	for t.pos < t.line.length()-1 {
		var tk *token

		switch {
		case inmultilinecomment:
			tk = t.readblockcomment(0, false)
			inmultilinecomment = !tk.blockcommentterminated
		case inmultilinestring:
			tk = t.readmultilinestring(0, false, multilinestrstart, multilinestrend)
			inmultilinestring = !tk.multilinestrterminated
			multilinestrstart = tk.multilinestrstart
			multilinestrend = tk.multilinestrend
		default:
			tk = t.nexttoken()
			if tk.typ == tk_blockcomment {
				inmultilinecomment = !tk.blockcommentterminated
			} else if tk.typ == tk_multilinestring {
				inmultilinestring = !tk.multilinestrterminated
				multilinestrstart = tk.multilinestrstart
				multilinestrend = tk.multilinestrend
			}
		}

		tokens = append(tokens, tk)
	}

	tokens = append(tokens, &token{typ: tk_nl, start: t.line.length(), end: t.line.length() - 1})

	return tokens, &lineattribute{nil, inmultilinecomment, inmultilinestring, multilinestrstart, multilinestrend}
}

func (t *clikelanglinetokenizer) nexttoken() *token {
	start := t.pos
	c := t.line.buffer[start]

	if c.tab || unicode.IsSpace(c.r) {
		return t.readwhitespace(start)
	}

	// comment
	switch len(t.linecommentstart) {
	case 1:
		if c.r == t.linecommentstart[0] {
			return t.readlinecomment(start)
		}
	case 2:
		if c.r == t.linecommentstart[0] && t.peek().r == t.linecommentstart[1] {
			return t.readlinecomment(start)
		}
	}

	switch len(t.blockcommentstart) {
	case 2:
		if c.r == t.blockcommentstart[0] && t.peek().r == t.blockcommentstart[1] {
			return t.readblockcomment(start, true)
		}
	}

	for i, multilinestringstart := range t.multilinestringstarts {
		switch len(multilinestringstart) {
		case 1:
			if c.r == multilinestringstart[0] {
				return t.readmultilinestring(start, true, multilinestringstart, t.multilinestringends[i])
			}
		case 2:
			if c.r == multilinestringstart[0] && t.peek().r == multilinestringstart[1] {
				return t.readmultilinestring(start, true, multilinestringstart, t.multilinestringends[i])
			}
		case 3:
			if c.r == multilinestringstart[0] && t.peek().r == multilinestringstart[1] && t.peek2().r == multilinestringstart[2] {
				return t.readmultilinestring(start, true, multilinestringstart, t.multilinestringends[i])
			}
		}
	}

	for i, stringstart := range t.stringstarts {
		switch len(stringstart) {
		case 1:
			if c.r == stringstart[0] {
				return t.readstring(start, stringstart, t.stringends[i], true)
			}
		case 2:
			if c.r == stringstart[0] && t.peek().r == stringstart[1] {
				return t.readstring(start, stringstart, t.stringends[i], true)
			}
		case 3:
			if c.r == stringstart[0] && t.peek().r == stringstart[1] && t.peek2().r == stringstart[2] {
				return t.readstring(start, stringstart, t.stringends[i], true)
			}
		}
	}

	for i, rawstringstart := range t.rawstringstarts {
		switch len(rawstringstart) {
		case 1:
			if c.r == rawstringstart[0] {
				return t.readstring(start, rawstringstart, t.rawstringends[i], false)
			}
		case 2:
			if c.r == rawstringstart[0] && t.peek().r == rawstringstart[1] {
				return t.readstring(start, rawstringstart, t.rawstringends[i], false)
			}
		case 3:
			if c.r == rawstringstart[0] && t.peek().r == rawstringstart[1] && t.peek2().r == rawstringstart[2] {
				return t.readstring(start, rawstringstart, t.rawstringends[i], false)
			}
		}
	}

	if unicode.IsDigit(c.r) || (c.r == '.' && unicode.IsDigit(t.peek().r)) {
		return t.readnumber(start)
	}

	if unicode.IsLetter(c.r) || c.r == '_' {
		return t.readident(start)
	}

	return t.readsymbol(start)
}

func (t *clikelanglinetokenizer) peek() *character {
	if t.line.length() <= t.pos+1 {
		return &character{}
	}
	return t.line.buffer[t.pos+1]
}

func (t *clikelanglinetokenizer) peek2() *character {
	if t.line.length() <= t.pos+2 {
		return &character{}
	}
	return t.line.buffer[t.pos+2]
}

func (t *clikelanglinetokenizer) readwhitespace(start int) *token {
	for t.pos < t.line.length()-1 && (unicode.IsSpace(t.line.buffer[t.pos].r) || t.line.buffer[t.pos].tab) {
		t.pos++
	}
	return &token{typ: tk_whitespace, start: start, end: t.pos}
}

func (t *clikelanglinetokenizer) readlinecomment(start int) *token {
	t.pos += len(t.linecommentstart)

	for t.pos < t.line.length()-1 { // skip nl
		t.pos++
	}

	return &token{typ: tk_linecomment, start: start, end: t.pos}
}

func (t *clikelanglinetokenizer) readblockcomment(start int, firstline bool) *token {
	if firstline {
		t.pos += len(t.blockcommentstart)
	}

	terminated := false
	for t.pos < t.line.length()-1 { // skip nl
		if t.line.length()-1 < t.pos+len(t.blockcommentend) {
			t.pos++
			continue
		}

		found := true
		for i, commentchar := range t.blockcommentend {
			if t.line.buffer[t.pos+i].r != commentchar {
				found = false
				break
			}
		}

		if found {
			t.pos += len(t.blockcommentend)
			terminated = true
			break
		}

		t.pos++
	}

	return &token{typ: tk_blockcomment, start: start, end: t.pos, blockcommentterminated: terminated}
}

func (t *clikelanglinetokenizer) readstring(start int, startquote, endquote []rune, considerescape bool) *token {
	t.pos += len(startquote)

	for t.pos < t.line.length()-1 { // skip nl
		if t.line.length()-1 < t.pos+len(endquote) {
			t.pos++
			continue
		}

		found := true
		for i, quotechar := range endquote {
			if t.line.buffer[t.pos+i].r != quotechar {
				found = false
				break
			}
		}
		if found {
			t.pos += len(endquote)
			break
		}

		if considerescape {
			if t.line.buffer[t.pos].r == '\\' && t.pos+1 < t.line.length()-1 {
				t.pos += 2
			} else {
				t.pos++
			}
		} else {
			t.pos++
		}
	}

	return &token{typ: tk_string, start: start, end: t.pos}
}

func (t *clikelanglinetokenizer) readmultilinestring(start int, firstline bool, startquote, endquote []rune) *token {
	if firstline {
		t.pos += len(startquote)
	}

	terminated := false
	for t.pos < t.line.length()-1 { // skip nl
		if t.line.length()-1 < t.pos+len(endquote) {
			t.pos++
			continue
		}

		found := true
		for i, quotechar := range endquote {
			if t.line.buffer[t.pos+i].r != quotechar {
				found = false
				break
			}
		}

		if found {
			t.pos += len(endquote)
			terminated = true
			break
		}

		t.pos++
	}

	return &token{typ: tk_multilinestring, start: start, end: t.pos, multilinestrterminated: terminated, multilinestrstart: startquote, multilinestrend: endquote}
}

func (t *clikelanglinetokenizer) readnumber(start int) *token {
	for t.pos < t.line.length() && (unicode.IsDigit(t.line.buffer[t.pos].r) || t.line.buffer[t.pos].r == '_') {
		t.pos++
	}

	if t.pos < t.line.length() && t.line.buffer[t.pos].r == '.' {
		t.pos++
		for t.pos < t.line.length() && (unicode.IsDigit(t.line.buffer[t.pos].r) || t.line.buffer[t.pos].r == '_') {
			t.pos++
		}
	}

	if t.pos < t.line.length() && (t.line.buffer[t.pos].r == 'e' || t.line.buffer[t.pos].r == 'E') {
		t.pos++
		if t.pos < t.line.length() && (t.line.buffer[t.pos].r == '+' || t.line.buffer[t.pos].r == '-') {
			t.pos++
		}
		for t.pos < t.line.length() && (unicode.IsDigit(t.line.buffer[t.pos].r) || t.line.buffer[t.pos].r == '_') {
			t.pos++
		}
	}

	return &token{typ: tk_number, start: start, end: t.pos}
}

func (t *clikelanglinetokenizer) readident(start int) *token {
	for t.pos < t.line.length() && (unicode.IsLetter(t.line.buffer[t.pos].r) || unicode.IsDigit(t.line.buffer[t.pos].r) || t.line.buffer[t.pos].r == '_') {
		t.pos++
	}

	typ := tk_ident

	s := t.line.substring(start, t.pos)
	if slices.Contains(t.keywords, s) {
		typ = tk_keyword
	}

	return &token{typ: typ, start: start, end: t.pos}
}

func (t *clikelanglinetokenizer) readsymbol(start int) *token {
	if t.pos+3 < t.line.length()-1 {
		threechars := t.line.substring(t.pos, t.pos+3)
		if slices.Contains(t.operators3, threechars) {
			t.pos += 3
			return &token{typ: tk_operator, start: start, end: t.pos}
		}
	}

	if t.pos+2 < t.line.length()-1 {
		twochars := t.line.substring(t.pos, t.pos+2)
		if slices.Contains(t.operators2, twochars) {
			t.pos += 2
			return &token{typ: tk_operator, start: start, end: t.pos}
		}
	}

	char := string(t.line.buffer[t.pos].r)
	if slices.Contains(t.operators, char) {
		t.pos++
		return &token{typ: tk_operator, start: start, end: t.pos}
	}

	if slices.Contains(t.symbols, char) {
		t.pos++
		return &token{typ: tk_symbol, start: start, end: t.pos}
	}

	t.pos++
	return &token{typ: tk_unknown, start: start, end: t.pos}
}

/*
 * screen
 */

type file interface {
	io.ReadWriteCloser
	Name() string
	Truncate(size int64) error
	Seek(offset int64, whence int) (int64, error)
}

type lineattribute struct {
	colors            []int
	inblockcomment    bool
	inmultilinestr    bool
	multilinestrstart []rune
	multilinestrend   []rune
}

func (s *lineattribute) String() string {
	switch {
	case s.inblockcomment:
		return fmt.Sprintf("{block comment line, colors: %v}", s.colors)
	case s.inmultilinestr:
		return fmt.Sprintf("{multi-line string line (quote: %v%v), colors: %v}", string(s.multilinestrstart), string(s.multilinestrend), s.colors)
	default:
		return fmt.Sprintf("{normal line: colors: %v}", s.colors)
	}
}

type cursor struct {
	x       int
	y       int
	actualx int
}

func (c *cursor) String() string {
	return fmt.Sprintf("{x: %v, y: %v, actualx: %v}", c.x, c.y, c.actualx)
}

type screen struct {
	term            *screenterm
	highlighter     highlighter
	width           int
	height          int
	lines           []*line
	lineattrs       []*lineattribute
	file            file
	linenumberwidth int

	yankedch *character

	cursors []*cursor

	xoffset int
	yoffset int

	scrolled              bool
	linestoberendered     []int
	highlightupdatedlines []int

	dirty bool
}

func newscreen(term terminal, x, y, width, height int, file file, theme *theme) *screen {
	s := &screen{
		term:    &screenterm{term: term, width: width, x: x, y: y},
		height:  height,
		width:   width,
		file:    file,
		cursors: []*cursor{{0, 0, 0}},
		xoffset: 0,
		yoffset: 0,
		lines:   []*line{},
	}

	// read file and initialize s.lines
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		s.lines = append(s.lines, newline(line))
	}

	if len(s.lines) == 0 {
		s.lines = []*line{newemptyline()}
	}

	// initialize line attribute
	s.lineattrs = make([]*lineattribute, len(s.lines))

	golangexts := []string{"go", "go_"} // for test
	pythonexts := []string{"py", "pyi"}

	ext := strings.TrimPrefix(filepath.Ext(file.Name()), ".")
	switch {
	case slices.Contains(golangexts, ext):
		s.highlighter = newgolanghighlighter(theme)

	case slices.Contains(pythonexts, ext):
		s.highlighter = newpythonhighlighter(theme)

	default:
		s.highlighter = nophighlighter{}
	}

	for i := range s.lines {
		prevlinestate := &lineattribute{}
		if i != 0 {
			prevlinestate = s.lineattrs[i-1]
		}
		s.lineattrs[i] = s.highlighter.highlightline(s.lines[i], prevlinestate)
	}

	// calculate line number area width
	s.updatelinenumberwidth()

	return s
}

func (s *screen) updatelinenumberwidth() {
	if len(s.lines) < 10000 {
		s.linenumberwidth = 4
		return
	}

	s.linenumberwidth = calcdigit(len(s.lines))
}

func calcdigit(n int) int {
	digit := 0
	for {
		n /= 10
		digit++
		if n == 0 {
			break
		}
	}
	return digit
}

func (s *screen) statusline() *line {
	return newline(fmt.Sprintf(" %v", s.file.Name()))
}

func (s *screen) curline(c *cursor) *line {
	return s.lines[c.y]
}

func (s *screen) render(force bool) {
	maincursor := s.cursors[len(s.cursors)-1]

	// when the x is too right, set x to the line tail.
	// This must not change s.x because s.x should be kept when moving to another long line.
	x := min(maincursor.x, s.curline(maincursor).width()-1)

	/* scroll x */

	var scrolled bool

	xpad := 4
	xok := func() direction {
		// too left, scroll left
		if x-xpad < s.xoffset {
			if x < xpad && s.xoffset <= x {
				// too left but no enough space left
				return 0
			}
			return left
		}

		// too right, scroll right
		if s.xoffset+s.width-(s.linenumberwidth+1)-1 < x+xpad {
			if s.curline(maincursor).width()-1 < x+xpad && x <= s.xoffset+s.width-(s.linenumberwidth+1)-1 {
				// too right but no enough space right
				return 0
			}
			return right
		}

		return 0
	}

	for {
		dir := xok()
		if dir == 0 {
			break
		}

		if dir == left {
			s.xoffset -= 1
		} else {
			s.xoffset += 1
		}
		scrolled = true
	}

	/* scroll y */

	ypad := 4
	yok := func() direction {
		padup := maincursor.y - s.yoffset
		paddown := (s.yoffset + s.height - 2) - maincursor.y

		// cursor has up and down padding, it's ok
		if padup >= ypad && paddown >= ypad {
			return 0
		}

		// up scroll needed
		if padup < ypad {
			// if y cursor is not shown, scroll up. This happens after some goto cmd.
			if padup < 0 {
				return up
			}

			// no enough pad above but cannot scroll up more, stop.
			if s.yoffset == 0 {
				return 0
			}

			return up
		}

		if paddown < ypad {
			if paddown < 0 {
				return down
			}

			if s.yoffset+s.height-1 == len(s.lines) {
				return 0
			}

			return down
		}

		return 0
	}

	for {
		dir := yok()
		if dir == 0 {
			break
		}

		if dir == up {
			s.yoffset -= 1
		} else {
			s.yoffset += 1
		}
		scrolled = true
	}

	type _cursor struct {
		c       *cursor
		charidx int
	}

	_cursors := make([]*_cursor, len(s.cursors))

	for i := range s.cursors {
		x := min(s.cursors[i].x, s.curline(s.cursors[i]).width()-1)
		s.cursors[i].actualx = x - s.xoffset + s.linenumberwidth + 1
		_cursors[i] = &_cursor{c: s.cursors[i], charidx: s.curline(s.cursors[i]).charidx(x, s.xoffset)}
	}

	/* update texts */

	displayline := func(y int) []byte {
		line := s.lines[y]
		linenumber := fmt.Sprintf("%v\x1b[38;5;243m%v\x1b[0m", strings.Repeat(" ", s.linenumberwidth-calcdigit(y+1)), y+1)

		cursor := []int{}
		for _, c := range _cursors {
			if c.c.y != y {
				continue
			}

			cursor = append(cursor, c.charidx)
		}
		return []byte(linenumber + " " + line.cutandcolorize(s.xoffset, s.width-1-(s.linenumberwidth+1), s.lineattrs[y].colors, cursor))
	}

	if scrolled || s.scrolled || force {
		// update all lines
		for i := range s.height - 1 {
			s.term.clearline(i)
			if s.yoffset+i < len(s.lines) {
				s.term.write(displayline(s.yoffset + i))
			}
		}
	} else if len(s.linestoberendered) != 0 || len(s.highlightupdatedlines) != 0 {
		// udpate only changed lines
		lines := slices.Concat(s.linestoberendered, s.highlightupdatedlines)
		slices.Sort(lines)
		lines = slices.Compact(lines)
		for _, l := range lines {
			// if changed line is not shown on the screen, skip
			if l < s.yoffset || s.height-2 < l-s.yoffset {
				continue
			}

			s.term.clearline(l - s.yoffset)

			if l <= len(s.lines)-1 {
				s.term.write(displayline(l))
			}
		}
	}

	// render status line
	s.term.clearline(s.height - 1)
	s.term.write([]byte(s.statusline().cutandcolorize(0, s.width-(s.linenumberwidth+1), []int{}, []int{})))
	s.term.flush()
	s.linestoberendered = []int{}
	s.highlightupdatedlines = []int{}
	s.scrolled = false
}

func (s *screen) highlightchangedlines() {
	if len(s.linestoberendered) == 0 {
		return
	}

	slices.Sort(s.linestoberendered)
	s.linestoberendered = slices.Compact(s.linestoberendered)

	for i := s.linestoberendered[0]; i < len(s.lines); i++ {
		prevlinestate := &lineattribute{}
		if i != 0 {
			prevlinestate = s.lineattrs[i-1]
		}

		newlineattr := s.highlighter.highlightline(s.lines[i], prevlinestate)
		curlineattr := s.lineattrs[i]
		s.highlightupdatedlines = append(s.highlightupdatedlines, i)

		// when the line state is not changed, the rest lines must not be changed also, so break the loop
		if s.linestoberendered[len(s.linestoberendered)-1] < i && curlineattr.inblockcomment == newlineattr.inblockcomment && curlineattr.inmultilinestr == newlineattr.inmultilinestr {
			s.lineattrs[i] = newlineattr
			break
		}

		s.lineattrs[i] = newlineattr
	}
}

func (s *screen) cleanupcursors() {
	slices.SortFunc(s.cursors, func(c1, c2 *cursor) int {
		if c1.x == c2.x && c1.y == c2.y {
			return 0
		}

		if c1.y != c2.y {
			if c1.y > c2.y {
				return 1
			} else {
				return -1
			}
		}

		if c1.x > c2.x {
			return 1
		}

		return -1
	})
	s.cursors = slices.CompactFunc(s.cursors, func(c1, c2 *cursor) bool {
		return c1.x == c2.x && c1.y == c2.y
	})
}

func (s *screen) handle(curmode mode, buff *input, reader *reader) mode {
	numinput := false
	num := 1
	isnum, n := buff.isnumber()
	if curmode == normal && isnum {
		numinput = true
		num = n
		for {
			next := reader.read()
			isnum2, n2 := next.isnumber()
			if !isnum2 {
				buff = next
				break
			}

			num *= 10
			num += n2
		}
	}

	if num == 0 {
		num = 1
	}

	newmode := curmode

	switch curmode {
	case normal:
		switch buff.special {
		case _left:
			s.movecursors(left, num)

		case _down:
			s.movecursors(down, num)

		case _up:
			s.movecursors(up, num)

		case _right:
			s.movecursors(right, num)

		case _ctrl_u:
			s.scrollhalf(up)

		case _ctrl_d:
			s.scrollhalf(down)

		case _not_special_key:
			switch buff.r {
			// case '\\':
			// 	debug(0, "debug line (%v): '%v', attr: %v\n", s.y, s.curline(), s.lineattrs[s.y])
			case 'C':
				s.addcursorbelow()

			case ',':
				s.deletecursors()

			case 'd':
				s.deletecursorchar()

			case 'o':
				s.insertlinefromcursors(down)
				newmode = insert

			case 'O':
				s.insertlinefromcursors(up)
				newmode = insert

			case 'G':
				if numinput {
					s.movecursorstoline(num)
				}

			/*
			 * goto mode
			 */
			case 'g':
				input2 := reader.read()
				switch input2.r {
				case 'g':
					s.movecursorstotopleft()

				case 'e':
					s.movecursorstobottomleft()

				case 'l':
					s.movecursorstolinebottom()

				case 's':
					s.movecursorstononspacelinehead()

				case 'h':
					s.movecursorstolinehead()

				default:
					// do nothing
				}

			case 'f':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.movecursorstonextch(newcharacter(input2.r))
				}

			case 'F':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.movecursorstoprevch(newcharacter(input2.r))
				}

			case 'r':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.replacecursorchar(newcharacter(input2.r))
				}

			case 'h':
				s.movecursors(left, num)

			case 'j':
				s.movecursors(down, num)

			case 'k':
				s.movecursors(up, num)

			case 'l':
				s.movecursors(right, num)

			}

		}

	case insert:
		switch buff.special {
		case _left:
			s.movecursors(left, 1)

		case _down:
			s.movecursors(down, 1)

		case _up:
			s.movecursors(up, 1)

		case _right:
			s.movecursors(right, 1)

		case _esc:
			newmode = normal

		case _cr:
			s.splitcursorsline()

		case _bs:
			s.deletecursorprevchar()

		case _tab:
			s.insertcharsatcursors([]*character{newcharacter('\t')})

		case _not_special_key:
			s.insertcharsatcursors([]*character{newcharacter(buff.r)})
		}

	default:
		panic(fmt.Sprintf("cannot handle mode %v", curmode))
	}

	s.highlightchangedlines()
	s.cleanupcursors()
	return newmode
}

func (s *screen) String() string {
	return fmt.Sprintf("{file: %v, term: %v, w: %v, h: %v, xoffset: %v, yoffset: %v}", s.file.Name(), s.term, s.width, s.height, s.xoffset, s.yoffset)
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (d direction) String() string {
	switch d {
	case up:
		return "up"
	case down:
		return "down"
	case left:
		return "left"
	case right:
		return "right"
	case 0:
		return "not_set"
	default:
		panic("unknown direction")
	}
}

/* scroll */

func (s *screen) scrollhalf(direction direction) {
	s.scrolled = true
	move := (s.height - 1) / 2
	switch direction {
	case up:
		s.yoffset = max(0, s.yoffset-move)
		s.movecursorsfunc(func(c *cursor) (int, int) {
			return c.x, max(0, c.y-move)
		})

	case down:
		s.yoffset = min(len(s.lines)-1, s.yoffset+move)
		s.movecursorsfunc(func(c *cursor) (int, int) {
			return c.x, min(len(s.lines)-1, c.y+move)
		})
	default:
		panic("invalid direction is passed")
	}
}

/* cursor movement */

func (s *screen) movecursorsfunc(f func(c *cursor) (int, int)) {
	for i := range s.cursors {
		s.movecursorfunc(s.cursors[i], f)
	}
}

func (s *screen) movecursorfunc(c *cursor, f func(c *cursor) (int, int)) {
	cury := c.y
	s.registerRenderLine(cury)
	c.x, c.y = f(c)
	if c.y != cury {
		s.registerRenderLine(c.y)
	}
}

func (s *screen) _movecursor(c *cursor, direction direction, cnt int) (int, int) {
	switch direction {
	case up:
		return c.x, max(c.y-cnt, 0)

	case down:
		return c.x, min(c.y+cnt, len(s.lines)-1)

	case left:
		nextx := max(0, s.xidx(c)-cnt)
		return s.curline(c).widthto(nextx), c.y

	case right:
		nextx := min(s.curline(c).length()-1, s.xidx(c)+cnt)
		return s.curline(c).widthto(nextx), c.y

	default:
		panic("invalid direction is passed")
	}
}

func (s *screen) movecursors(direction direction, cnt int) {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		return s._movecursor(c, direction, cnt)
	})
}

func (s *screen) movecursor(c *cursor, direction direction, cnt int) {
	s.movecursorfunc(c, func(c *cursor) (int, int) {
		return s._movecursor(c, direction, cnt)
	})
}

func (s *screen) movecursorstonextch(ch *character) {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		line := s.curline(c)
		newx := c.x
		for i := s.xidx(c) + 1; i < line.length(); i++ {
			if line.buffer[i].equal(ch) {
				newx = line.widthto(i)
				break
			}
		}
		// if ch is not found, newx is still the original x, so not moved
		return newx, c.y
	})
}

func (s *screen) movecursorstoprevch(ch *character) {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		line := s.curline(c)
		newx := c.x
		for i := s.xidx(c) - 1; 0 <= i; i-- {
			if line.buffer[i].equal(ch) {
				newx = line.widthto(i)
				break
			}
		}
		// if ch is not found, newx is still the original x, so not moved
		return newx, c.y
	})
}

func (s *screen) movecursorstotopleft() {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		return 0, 0
	})
}

func (s *screen) movecursorstobottomleft() {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		return 0, len(s.lines) - 1
	})
}

func (s *screen) movecursorstolinebottom() {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		return s.curline(c).width() - 1, c.y
	})
}

func (s *screen) movecursorstononspacelinehead() {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		x := 0
		curline := s.curline(c)
		for i := range curline.buffer {
			if !curline.buffer[i].isspace() {
				break
			}
			x += curline.buffer[i].width
		}
		return x, c.y
	})
}

func (s *screen) movecursorstolinehead() {
	s.movecursorsfunc(func(c *cursor) (int, int) {
		return 0, c.y
	})
}

func (s *screen) movecursorstoline(line int) {
	if len(s.lines) < line {
		line = len(s.lines)
	}

	s.movecursorsfunc(func(c *cursor) (int, int) {
		return 0, line - 1
	})
}

/* cursor manipulation */

func (s *screen) shiftcursors(direction direction, from, amount int) {
	for i := from; i < len(s.cursors); i++ {
		s.registerRenderLine(s.cursors[i].y)
		switch direction {
		case up:
			s.cursors[i].y -= amount
		case down:
			s.cursors[i].y += amount
		default:
			panic("invalid direction passed")
		}
		s.registerRenderLine(s.cursors[i].y)
	}
}

func (s *screen) addcursorbelow() {
	lastcursor := s.cursors[len(s.cursors)-1]
	for i := lastcursor.y + 1; i < len(s.lines); i++ {
		if lastcursor.x < s.lines[i].width() {
			s.cursors = append(s.cursors, &cursor{x: lastcursor.x, y: i, actualx: lastcursor.actualx})
			s.registerRenderLine(i)
			break
		}
	}
}

func (s *screen) deletecursors() {
	for i, c := range s.cursors {
		if i == len(s.cursors)-1 {
			break
		}
		s.registerRenderLine(c.y)
	}
	s.cursors = slices.Delete(s.cursors, 0, len(s.cursors)-1)
}

/* text modification */

func (s *screen) insertcharsatcursors(chars []*character) {
	for _, c := range s.cursors {
		s.alignx(c)
		s.curline(c).inschars(chars, s.xidx(c))
		s.movecursors(right, len(chars))
		s.registerRenderLine(c.y)
	}
}

func (s *screen) deletecursorchar() {
	for i, c := range s.cursors {
		if s.atlinetail(c) && c.y+1 < len(s.lines) {
			// when removing nl, concat current and next line
			s.joinlines(c.y, c.y+1)
			s.shiftcursors(up, i+1, 1)
		} else {
			s.curline(c).delchar(s.xidx(c))
			s.dirty = true
			s.registerRenderLine(c.y)
		}
	}
}

func (s *screen) deletecursorprevchar() {
	for i, c := range s.cursors {
		switch s.xidx(c) {
		case 0:
			if c.y != 0 {
				s.registerRenderLineAfter(c.y)
				// join current and above line
				// next x is right edge on the above line
				nextx := s.lines[c.y-1].width() - 1
				s.joinlines(c.y-1, c.y)
				s.movecursor(c, up, 1)
				c.x = nextx
				s.shiftcursors(up, i, 1)
			}

		default:
			// just delete the char

			// move cursor before deleting char to prevent
			// the cursor points nowhere after deleting the rightmost char.
			s.movecursor(c, left, 1)
			s.curline(c).delchar(s.xidx(c) - 1)
			s.registerRenderLine(c.y)
		}
	}
}

func (s *screen) insertlinefromcursors(direction direction) {
	for i, c := range s.cursors {
		s.insline(c, direction)
		s.shiftcursors(down, i+1, 1)
		s.movecursor(c, direction, 1)
	}
}

func (s *screen) insline(c *cursor, direction direction) {
	switch direction {
	case up:
		s.lines = slices.Insert(s.lines, c.y, newemptyline())
		s.lineattrs = slices.Insert(s.lineattrs, c.y, &lineattribute{})
	case down:
		s.lines = slices.Insert(s.lines, c.y+1, newemptyline())
		s.lineattrs = slices.Insert(s.lineattrs, c.y+1, &lineattribute{})
	default:
		panic("invalid direction is passed")
	}

	s.registerRenderLineAfter(c.y)
	s.dirty = true
	s.updatelinenumberwidth()
}

func (s *screen) delline(y int) {
	s.registerRenderLineAfter(y)
	s.lines = slices.Delete(s.lines, y, y+1)
	s.lineattrs = slices.Delete(s.lineattrs, y, y+1)
	s.updatelinenumberwidth()
}

func (s *screen) replacecursorchar(ch *character) {
	for _, c := range s.cursors {
		s.curline(c).replacech(ch, s.xidx(c))
		s.registerRenderLine(c.y)
	}
	s.dirty = true
}

// from, to both inclusive
func (s *screen) joinlines(from, to int) {
	// first, append lines to the base line
	for i := from + 1; i <= to; i++ {
		s.lines[from].delnl()
		s.lines[from].buffer = append(s.lines[from].buffer, s.lines[i].buffer...)
	}

	// then, delete joined lines
	for i := from + 1; i <= to; i++ {
		s.delline(i)
	}

	s.registerRenderLineAfter(from)
	s.dirty = true
	s.updatelinenumberwidth()
}

func (s *screen) splitcursorsline() {
	for i, c := range s.cursors {
		s.registerRenderLineAfter(c.y)

		curline := s.curline(c).copy()
		nextline := s.curline(c).copy()
		curidx := s.xidx(c)

		s.curline(c).clear()
		s.curline(c).inschars(curline.buffer[:curidx], 0)

		s.insline(c, down)
		s.movecursor(c, down, 1)
		s.curline(c).inschars(nextline.buffer[curidx:len(nextline.buffer)-1], 0)
		c.x = 0

		s.shiftcursors(down, i+1, 1)
	}
	s.dirty = true
}

/* helpers */

func (s *screen) atlinetail(c *cursor) bool {
	return s.xidx(c) == s.curline(c).length()-1
}

// return x character index from the current cursor position on screen
func (s *screen) xidx(c *cursor) int {
	return s.curline(c).charidx(c.actualx-s.linenumberwidth-1, s.xoffset)
}

// ensure current s.x is pointing on the correct character position.
// if x is too right after up/down move, fix x position.
// if x is not aligning to the multi length character head, align there.
func (s *screen) alignx(c *cursor) {
	c.x = s.curline(c).widthto(s.xidx(c))
}

func (s *screen) registerRenderLine(y int) {
	s.linestoberendered = append(s.linestoberendered, y)
}

func (s *screen) registerRenderLineAfter(after int) {
	for i := after; i < len(s.lines); i++ {
		s.linestoberendered = append(s.linestoberendered, i)
	}
}

/* file persistence */

func (s *screen) content() []byte {
	buf := []byte{}
	for _, line := range s.lines {
		for _, ch := range line.buffer {
			switch {
			case ch.tab:
				buf = append(buf, '\t')
			case ch.nl:
				buf = append(buf, '\n')
			default:
				buf = append(buf, []byte(string(ch.r))...)
			}
		}
	}
	return buf
}

func (s *screen) save() {
	content := s.content()
	s.file.Truncate(0)
	s.file.Seek(0, 0)
	_, err := s.file.Write([]byte(content))
	if err != nil {
		panic(err)
	}
	s.dirty = false
}

/*
 * window
 */

type window struct {
	x         int
	y         int
	width     int
	height    int
	parent    *window
	children  []*window
	screen    *screen
	direction direction // down or right
}

func newleafwindow(term terminal, x, y, width, height int, file file, theme *theme) *window {
	return &window{
		x:      x,
		y:      y,
		width:  width,
		height: height,
		screen: newscreen(term, x, y, width, height, file, theme),
	}
}

func (w *window) isroot() bool {
	return w.parent == nil
}

func (w *window) isleaf() bool {
	return len(w.children) == 0
}

func (w *window) split(term terminal, direction direction, file file, theme *theme) *window {
	// when the given directions is the same with parent window, add new window as sibling of w.
	if w.parent != nil && w.parent.direction == direction {
		return w.parent.inschildafter(w, term, file, theme)
	}

	// when no parent exists (= w is root) or exists but direction is different,
	// make the leaf window w to inner window, then add new window as child.
	w.toinner(direction)
	return w.inschildafter(w.children[0], term, file, theme)
}

func (w *window) toinner(direction direction) {
	// make leaf node to inner node
	if !w.isleaf() {
		panic("toinner is called on non-leaf window")
	}

	w.direction = direction
	w.children = []*window{{parent: w, screen: w.screen}}
	w.screen = nil
}

func (w *window) inschildafter(after *window, term terminal, file file, theme *theme) *window {
	// insert a child node after $after then do resize.
	newwin := newleafwindow(term, 0, 0, 0, 0, file, theme)
	newwin.parent = w
	idx := slices.Index(w.children, after)
	if idx == -1 {
		panic("cannot find a given child node in the children")
	}

	w.children = slices.Insert(w.children, idx+1, newwin)
	w.resizechildren()
	return newwin
}

func (w *window) resizechildren() {
	// divide the $total into $count items so that they are all the same length
	// as much as possible. This considers splitter sign (| or -).
	//
	// suppose total is 30, count is 4, then
	//    7   |   7   |   7   |   6   (7 + 7 + 7 + 6 + 3(splitter) = 30)
	// AAAAAAA|BBBBBBB|CCCCCCC|DDDDDD
	// is expected (| sign is window splitter).
	f := func(total, count int) []int {
		total -= count - 1
		result := make([]int, count)
		for i := range count {
			_mod := total % (count - i)
			_div := total / (count - i)
			if _mod == 0 {
				result[i] = _div
				total -= _div
			} else {
				result[i] = _div + 1
				total -= _div + 1
			}
		}
		return result
	}

	sum := func(ints []int) int {
		s := 0
		for i := range ints {
			s += ints[i]
		}
		return s
	}

	switch w.direction {
	case right:
		widths := f(w.width, len(w.children))
		for i := range w.children {
			w.children[i].changesize(w.x+sum(widths[:i])+1*i, w.y, widths[i], w.height)
		}

	case down:
		heights := f(w.height, len(w.children))
		for i := range w.children {
			w.children[i].changesize(w.x, w.y+sum(heights[:i])+1*i, w.width, heights[i])
		}

	default:
		panic("unexpected split direction")
	}
}

func (w *window) changesize(x, y, width, height int) {
	w.x = x
	w.y = y
	w.width = width
	w.height = height

	if w.isleaf() {
		w.screen.width = width
		w.screen.height = height
		w.screen.term.x = x
		w.screen.term.y = y
		w.screen.term.width = width
	} else {
		// when parent node size is changed, children size must be changed accordingly
		w.resizechildren()
	}
}

func (w *window) render(term *screenterm, first bool) {
	if w.isleaf() {
		w.screen.render(first)
		return
	}

	for i, child := range w.children {
		child.render(term, first)

		// add splitter line on non last window
		if i != len(w.children)-1 {
			if w.direction == right {
				for j := range child.height {
					term.putcursor(child.x+child.width, child.y+j)
					term.write([]byte("|"))
				}
			} else {
				for j := range child.width {
					term.putcursor(child.x+j, child.y+child.height)
					term.write([]byte("-"))
				}

			}
		}
	}
}

func (w *window) actualcursor() (int, int) {
	return w.x + w.screen.cursors[0].actualx, w.y + w.screen.cursors[0].y - w.screen.yoffset
}

func (w *window) getallleaves() []*window {
	if w.isleaf() {
		return []*window{w}
	}

	leaves := []*window{}
	for _, child := range w.children {
		if w.isleaf() {
			leaves = append(leaves, child)
			continue
		}

		leaves = append(leaves, child.getallleaves()...)
	}

	return leaves
}

func (w *window) close() *window {
	if w.isroot() {
		w.screen.file.Close()
		return nil
	}

	w.screen.file.Close()
	next := w.parent.removechild(w)
	if len(w.parent.children) == 1 {
		w.parent.toleaf()
		return w.parent
	}

	return next
}

func (w *window) toleaf() {
	w.screen = w.children[0].screen
	w.children = []*window{}
	w.direction = 0
}

func (w *window) removechild(child *window) *window {
	idx := slices.Index(w.children, child)
	if idx == -1 {
		panic("cannot find a given child node to remove in the children")
	}
	w.children = slices.Delete(w.children, idx, idx+1)
	w.resizechildren()

	idx = max(0, idx-1)

	if w.children[idx].isleaf() {
		return w.children[idx]
	}

	return w.children[idx].firstleaf()
}

func (w *window) firstleaf() *window {
	if w.children[0].isleaf() {
		return w.children[0]
	}
	return w.children[0].firstleaf()
}

func (w *window) String() string {
	return w.string(0)
}

func (w *window) string(depth int) string {
	var sb strings.Builder
	f0 := func(s string, a ...any) { sb.WriteString(fmt.Sprintf(s, a...)) }
	f := func(s string, a ...any) { sb.WriteString(strings.Repeat("  ", depth) + fmt.Sprintf(s, a...)) }
	f2 := func(s string, a ...any) { sb.WriteString(strings.Repeat("  ", depth+1) + fmt.Sprintf(s, a...)) }

	f("{\n")
	f2("x: %v, y: %v, width: %v, height: %v, direction: %v,\n", w.x, w.y, w.width, w.height, w.direction)
	f2("screen: %v\n", w.screen)
	f2("children : [")
	if len(w.children) == 0 {
		f0("]\n")
	} else {
		f0("\n")
		for _, child := range w.children {
			f0("%v\n", child.string(depth+2))
		}
		f2("]\n")
	}
	f("}")

	return sb.String()
}

/*
 * editor
 */

type editor struct {
	term          *screenterm
	theme         *theme
	rootwin       *window
	activewin     *window
	windowchanged bool
	height        int
	width         int
	mode          mode
	cmdline       *line
	cmdx          int
	msg           *line
}

func (e *editor) changemode(mode mode) {
	e.mode = mode
}

func (e *editor) vsplit(filename string) {
	e.split(filename, right)
}

func (e *editor) hsplit(filename string) {
	e.split(filename, down)
}

func (e *editor) split(filename string, direction direction) {
	if direction != down && direction != right {
		panic("unexpected direction to split")
	}

	_, err := os.Stat(filename)
	if err != nil {
		e.msg = newline(fmt.Sprintf("file not found: '%v'", filename))
		e.changemode(normal)
		return
	}
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		e.msg = newline(fmt.Sprintf("cannot open: '%v'", filename))
		e.changemode(normal)
		return
	}

	e.activewin = e.activewin.split(e.term.term, direction, file, e.theme)
	e.windowchanged = true
}

func (e *editor) jumpwin(direction direction) {
	// cursor
	x, y := e.activewin.actualcursor()
	leaves := e.rootwin.getallleaves()

	var candidate *window
	for _, leaf := range leaves {
		if leaf == e.activewin {
			continue
		}

		switch direction {
		case left:
			if x < leaf.x || y < leaf.y || leaf.y+leaf.height < y {
				continue
			}

			if candidate == nil || candidate.x < leaf.x {
				candidate = leaf
			}

		case right:
			if leaf.x+leaf.width < x || y < leaf.y || leaf.y+leaf.height < y {
				continue
			}

			if candidate == nil || leaf.x < candidate.x {
				candidate = leaf
			}

		case up:
			if y < leaf.y || x < leaf.x || leaf.x+leaf.width < x {
				continue
			}

			if candidate == nil || candidate.y < leaf.y {
				candidate = leaf
			}

		case down:
			if leaf.y+leaf.height < y || x < leaf.x || leaf.x+leaf.width < x {
				continue
			}

			if candidate == nil || leaf.y < candidate.y {
				candidate = leaf
			}
		}
	}

	if candidate == nil {
		return
	}

	e.activewin = candidate
}

func (e *editor) closewin() {
	if e.activewin.screen.dirty {
		e.msg = newline(fmt.Sprintf("unsaved change remaining: '%v'", e.activewin.screen.file.Name()))
		return
	}

	e.activewin = e.activewin.close()
	e.windowchanged = true
}

func (e *editor) closewinforce() {
	e.activewin = e.activewin.close()
	e.windowchanged = true
}

func (e *editor) movecmdcursor(direction direction) {
	switch direction {
	case left:
		nextx := max(0, e.cmdxidx()-1)
		e.cmdx = e.cmdline.widthto(nextx)
	case right:
		nextx := min(e.cmdline.length()-1, e.cmdxidx()+1)
		e.cmdx = e.cmdline.widthto(nextx)
	default:
		panic("invalid direction is passed")
	}
}

func (e *editor) cmdxidx() int {
	return e.cmdline.charidx(e.cmdx, 0)
}

func (e *editor) commandline() *line {
	if e.mode != command {
		return newemptyline()
	}

	return newline(fmt.Sprintf(":%v", e.cmdline))
}

func (e *editor) render(first bool) {
	// to prevent cursor flickering
	e.term.flush()

	/* update command line */
	e.term.clearline(e.height - 1)
	if !e.msg.empty() {
		e.term.write([]byte(e.msg.cutandcolorize(0, e.width, []int{}, []int{})))
	} else if e.mode == command {
		cursor := []int{e.cmdx + 1}
		cl := e.commandline()
		cl.delnl()
		e.term.write([]byte(cl.cutandcolorize(0, e.width, []int{}, cursor)))
	}

	if e.windowchanged {
		e.rootwin.render(e.term, true)
		e.activewin.screen.render(first)
	} else {
		e.activewin.render(e.term, first)
	}

	e.term.flush()

	e.windowchanged = false
}

func (e *editor) resetcmd() {
	e.cmdline = newcommandline()
	e.cmdx = 0
}

func (e *editor) save() {
	e.activewin.screen.save()
}

func (e *editor) String() string {
	var sb strings.Builder
	f := func(s string, a ...any) { sb.WriteString(fmt.Sprintf(s, a...)) }
	f2 := func(s string, a ...any) { sb.WriteString("  " + fmt.Sprintf(s, a...)) }

	f("[DEBUG] editor_state{\n")
	f2("term: %v,\n", e.term)
	f2("width: %v, height: %v, mode: %v, cmdline: '%v', cmdx: %v, msg: '%v'\n", e.width, e.height, e.mode, e.cmdline, e.cmdx, e.msg)
	f2("rootwin: %v,\n", e.rootwin.string(1))
	f2("activewin: {name: %v, x: %v, y: %v},\n", e.activewin.screen.file.Name(), e.activewin.x, e.activewin.y)
	f("}")

	return sb.String()
}

func (e *editor) debug() {
	debug(2, "%v\n", e)
}

func start(term terminal, in io.Reader, file file, theme *theme) {
	fin, err := term.init()
	if err != nil {
		fin()
		panic(err)
	}

	defer func() {
		fin()
		term.refresh()
		term.putcursor(0, 0)
		term.showcursor()
		term.flush()
	}()

	term.refresh()
	term.hidecursor()

	/*
	 * Prepare editor state
	 */

	height, width, err := term.windowsize()
	if err != nil {
		panic(err)
	}

	e := &editor{
		term:    newscreenterm(term, 0, 0, width),
		theme:   theme,
		height:  height,
		width:   width,
		mode:    normal,
		cmdline: newemptyline(),
		cmdx:    0,
		msg:     newemptyline(),
	}

	e.rootwin = newleafwindow(e.term.term, 0, 0, e.width, e.height-1, file, e.theme)
	e.activewin = e.rootwin
	e.render(true)

	/*
	 * start editor main routine
	 */

	reader := &reader{r: bufio.NewReader(in)}

	for {
		// reset message
		// this keeps showing the message just until the next input
		e.msg = newemptyline()

		buff := reader.read()

		switch e.mode {
		case command:
			switch buff.special {
			case _left:
				e.movecmdcursor(left)

			case _right:
				e.movecmdcursor(right)

			case _esc:
				e.resetcmd()
				e.changemode(normal)

			case _bs:
				if 0 < e.cmdx {
					e.movecmdcursor(left)
					e.cmdline.delchar(e.cmdxidx())
				}

			case _cr:
				switch {
				case e.cmdline.equal("q"):
					e.resetcmd()
					e.changemode(normal)
					e.closewin()
					if e.activewin == nil {
						goto finish
					}

				case e.cmdline.equal("q!"):
					e.resetcmd()
					e.changemode(normal)
					e.closewinforce()
					if e.activewin == nil {
						goto finish
					}

				case e.cmdline.hasprefix("vs "):
					filename := e.cmdline.trimprefix("vs ")
					e.vsplit(filename)
					e.resetcmd()
					e.changemode(normal)

				case e.cmdline.hasprefix("hs"):
					filename := e.cmdline.trimprefix("hs ")
					e.hsplit(filename)
					e.resetcmd()
					e.changemode(normal)

				case e.cmdline.equal("w"):
					e.save()
					e.msg = newline("saved!")
					e.resetcmd()
					e.changemode(normal)

				case e.cmdline.equal("wq"):
					e.save()
					e.resetcmd()
					goto finish

				default:
					e.msg = newline("unknown command!")
					e.resetcmd()
					e.changemode(normal)
				}

			case _not_special_key:
				e.cmdline.inschars([]*character{newcharacter(buff.r)}, e.cmdxidx())
				e.movecmdcursor(right)
			}

		case normal:
			switch buff.special {
			case _ctrl_w:
				input2 := reader.read()
				switch {
				case input2.r == 'h', input2.special == _ctrl_h, input2.special == _left:
					e.jumpwin(left)
				case input2.r == 'j', input2.special == _ctrl_j, input2.special == _down:
					e.jumpwin(down)
				case input2.r == 'k', input2.special == _ctrl_k, input2.special == _up:
					e.jumpwin(up)
				case input2.r == 'l', input2.special == _ctrl_l, input2.special == _right:
					e.jumpwin(right)
				default:
					// do nothing
				}
			case _not_special_key:
				switch buff.r {
				case ':':
					e.changemode(command)
				case 'i':
					e.changemode(insert)
				default:
					newmode := e.activewin.screen.handle(e.mode, buff, reader)
					e.changemode(newmode)
				}
			default:
				newmode := e.activewin.screen.handle(e.mode, buff, reader)
				e.changemode(newmode)
			}

		case insert:
			switch buff.special {
			case _esc:
				e.changemode(normal)
			default:
				newmode := e.activewin.screen.handle(e.mode, buff, reader)
				e.changemode(newmode)
			}

		default:
			panic("unknown mode")
		}

		e.render(false)
		e.debug()
	}

finish:
}

func main() {
	var (
		_theme = flag.String("theme", "doraemon", "theme name, choose from [doraemon, noby (or nobita), sue (or shizuka), sneech (or suneo), big-g (or gian)]")
	)
	flag.Parse()

	theme := theme_doraemon
	switch *_theme {
	case "doraemon":
		theme = theme_doraemon
	case "noby", "nobita":
		theme = theme_nobita
	case "sue", "shizuka":
		theme = theme_shizuka
	case "sneech", "suneo":
		theme = theme_suneo
	case "big-g", "gian":
		theme = theme_gian
	}

	args := flag.Args()

	var filename string
	switch len(args) {
	case 0:
		filename = filepath.Join(os.TempDir(), "__scratch__")
	case 1:
		filename = args[0]
	default:
		fmt.Println("more than 2 args are passed")
		return
	}

	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}

	start(&unixVT100term{}, os.Stdin, file, theme)
}

/*
 * keypress
 */

type input struct {
	r       rune
	special key
}

func (i *input) isnumber() (bool, int) {
	if i.special != _not_special_key {
		return false, 0
	}

	n := int(i.r - '0')
	if n < 0 || 9 < n {
		return false, 0
	}

	return true, n
}

func (i *input) String() string {
	if i.special == _not_special_key {
		return fmt.Sprintf("%v", string(i.r))
	}
	return fmt.Sprintf("%v", i.special)
}

type key int

func (k key) String() string {
	switch k {
	case _not_special_key:
		return "not_special_key"
	case _unknown:
		return "unknown"
	case _lf:
		return "LF"
	case _cr:
		return "CR"
	case _tab:
		return "TAB"
	case _esc:
		return "ESC"
	case _bs:
		return "BackSpace"
	case _up:
		return "up"
	case _down:
		return "down"
	case _right:
		return "right"
	case _left:
		return "left"
	case _home:
		return "Home"
	case _end:
		return "End"
	case _insert:
		return "Insert"
	case _del:
		return "Delete"
	case _pageup:
		return "PageUp"
	case _pagedown:
		return "PageDown"
	case _ctrl_a:
		return "Ctrl+a"
	case _ctrl_b:
		return "Ctrl+b"
	case _ctrl_c:
		return "Ctrl+c"
	case _ctrl_d:
		return "Ctrl+d"
	case _ctrl_e:
		return "Ctrl+e"
	case _ctrl_f:
		return "Ctrl+f"
	case _ctrl_g:
		return "Ctrl+g"
	case _ctrl_h:
		return "Ctrl+h"
	case _ctrl_i:
		return "Ctrl+i"
	case _ctrl_j:
		return "Ctrl+j"
	case _ctrl_k:
		return "Ctrl+k"
	case _ctrl_l:
		return "Ctrl+l"
	case _ctrl_m:
		return "Ctrl+m"
	case _ctrl_n:
		return "Ctrl+n"
	case _ctrl_o:
		return "Ctrl+o"
	case _ctrl_p:
		return "Ctrl+p"
	case _ctrl_q:
		return "Ctrl+q"
	case _ctrl_r:
		return "Ctrl+r"
	case _ctrl_s:
		return "Ctrl+s"
	case _ctrl_t:
		return "Ctrl+t"
	case _ctrl_u:
		return "Ctrl+u"
	case _ctrl_v:
		return "Ctrl+v"
	case _ctrl_w:
		return "Ctrl+w"
	case _ctrl_x:
		return "Ctrl+x"
	case _ctrl_y:
		return "Ctrl+y"
	case _ctrl_z:
		return "Ctrl+z"
	default:
		panic("unknown key")
	}
}

const (
	_not_special_key key = iota
	_unknown             // special key but could not handle
	_lf
	_cr
	_tab
	_esc
	_bs

	// arrow
	_up
	_down
	_right
	_left

	_home
	_end
	_insert
	_del
	_pageup
	_pagedown

	_ctrl_a
	_ctrl_b
	_ctrl_c
	_ctrl_d
	_ctrl_e
	_ctrl_f
	_ctrl_g
	_ctrl_h
	_ctrl_i
	_ctrl_j
	_ctrl_k
	_ctrl_l
	_ctrl_m
	_ctrl_n
	_ctrl_o
	_ctrl_p
	_ctrl_q
	_ctrl_r
	_ctrl_s
	_ctrl_t
	_ctrl_u
	_ctrl_v
	_ctrl_w
	_ctrl_x
	_ctrl_y
	_ctrl_z
)

type reader struct {
	r *bufio.Reader
}

func (r *reader) read() (i *input) {
	var dbg []byte

	defer func() {
		if i.special == _unknown {
			debug(1, "read: unknown input detected: %v\n", string(dbg))
		}
	}()

	first, err := r.r.ReadByte()
	if err != nil {
		panic(err)
	}

	dbg = append(dbg, first)

	buffered := r.r.Buffered()

	if first != 0x1b {
		buf := make([]byte, buffered)
		if buffered != 0 {
			_, err := r.r.Read(buf)
			if err != nil {
				panic(err)
			}
		}

		dbg = append(dbg, buf...)
		r, _ := utf8.DecodeRune(append([]byte{first}, buf...))

		switch r {
		case '\r':
			return &input{special: _cr}
		case '\t':
			return &input{special: _tab}
		case 127:
			return &input{special: _bs}
		default:
			if r < 32 {
				switch r {
				case 1:
					return &input{special: _ctrl_a}
				case 2:
					return &input{special: _ctrl_b}
				case 3:
					return &input{special: _ctrl_c}
				case 4:
					return &input{special: _ctrl_d}
				case 5:
					return &input{special: _ctrl_e}
				case 6:
					return &input{special: _ctrl_f}
				case 7:
					return &input{special: _ctrl_g}
				case 8:
					return &input{special: _ctrl_h}
				case 9:
					return &input{special: _ctrl_i}
				case 10:
					return &input{special: _ctrl_j}
				case 11:
					return &input{special: _ctrl_k}
				case 12:
					return &input{special: _ctrl_l}
				case 13:
					return &input{special: _ctrl_m}
				case 14:
					return &input{special: _ctrl_n}
				case 15:
					return &input{special: _ctrl_o}
				case 16:
					return &input{special: _ctrl_p}
				case 17:
					return &input{special: _ctrl_q}
				case 18:
					return &input{special: _ctrl_r}
				case 19:
					return &input{special: _ctrl_s}
				case 20:
					return &input{special: _ctrl_t}
				case 21:
					return &input{special: _ctrl_u}
				case 22:
					return &input{special: _ctrl_v}
				case 23:
					return &input{special: _ctrl_w}
				case 24:
					return &input{special: _ctrl_x}
				case 25:
					return &input{special: _ctrl_y}
				case 26:
					return &input{special: _ctrl_z}
				}
			}
			return &input{r: r}
		}
	}

	// when first byte is 0x1b but no more bytes buffered,
	// it's just [0x1b] input which is esc key.
	if buffered == 0 {
		return &input{special: _esc}
	}

	// escape sequence
	buf := make([]byte, buffered)
	n, err := r.r.Read(buf)
	if err != nil {
		panic(err)
	}
	dbg = append(dbg, buf...)

	if buf[0] == '[' {
		switch n {
		case 2:
			switch buf[1] {
			case 'A':
				return &input{special: _up}
			case 'B':
				return &input{special: _down}
			case 'C':
				return &input{special: _right}
			case 'D':
				return &input{special: _left}
			case 'H':
				return &input{special: _home}
			case 'F':
				return &input{special: _end}
			}

		case 3:
			switch {
			case buf[1] == '2' && buf[2] == '~':
				return &input{special: _insert}
			case buf[1] == '3' && buf[2] == '~':
				return &input{special: _del}
			case buf[1] == '5' && buf[2] == '~':
				return &input{special: _pageup}
			case buf[1] == '6' && buf[2] == '~':
				return &input{special: _pagedown}
			}
		}
	}

	return &input{special: _unknown}
}

func debug(level int, format string, a ...any) (int, error) {
	if level <= _debuglevel {
		return fmt.Fprintf(os.Stderr, format, a...)
	}
	return 0, nil
}

/*
 * abstract virtual terminal interface
 */

type screenterm struct {
	term terminal

	// screen position in the terminal screen
	x int
	y int

	// screen width
	width int
}

func newscreenterm(term terminal, x, y, width int) *screenterm {
	return &screenterm{term: term, x: x, y: y, width: width}
}

func (st *screenterm) String() string {
	return fmt.Sprintf("{x: %v, y: %v, width: %v}", st.x, st.y, st.width)
}

func (st *screenterm) init() (func(), error) {
	return st.term.init()
}

func (st *screenterm) windowsize() (int, int, error) {
	return st.term.windowsize()
}

func (st *screenterm) refresh() {
	st.term.refresh()
}

func (st *screenterm) hidecursor() {
	st.term.hidecursor()
}

func (st *screenterm) showcursor() {
	st.term.showcursor()
}

func (st *screenterm) clearline(y int) {
	st.putcursor(0, y)
	st.term.clearline(st.width)
	st.putcursor(0, y)
}

func (st *screenterm) putcursor(x, y int) {
	st.term.putcursor(st.x+x, st.y+y)
}

func (st *screenterm) write(b []byte) {
	st.term.write(b)
}

func (st *screenterm) flush() {
	st.term.flush()
}

/*
 * generic terminal
 */

type terminal interface {
	init() (func(), error)
	windowsize() (int, int, error)

	// do write buffering
	refresh()
	hidecursor()
	showcursor()
	clearline(width int)
	putcursor(x, y int)
	write(b []byte)

	// flushes the buffer
	flush()
}

type unixVT100term struct {
	w    io.Writer
	buff []byte // not using bufio.Writer as we want to control when the buffer is flushed
}

func (t *unixVT100term) init() (func(), error) {
	t.w = os.Stdout

	oldstate, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return func() {}, err
	}
	return func() { term.Restore(int(os.Stdin.Fd()), oldstate) }, nil
}

func (t *unixVT100term) windowsize() (int, int, error) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	return height, width, err
}

func (t *unixVT100term) refresh() {
	t.buff = fmt.Appendf(t.buff, "\x1b[2J")
}

func (t *unixVT100term) hidecursor() {
	t.buff = fmt.Appendf(t.buff, "\x1b[?25l")
}

func (t *unixVT100term) showcursor() {
	t.buff = fmt.Appendf(t.buff, "\x1b[?25h")
}

func (t *unixVT100term) clearline(width int) {
	t.buff = fmt.Appendf(t.buff, "%v", strings.Repeat(" ", width))
}

func (t *unixVT100term) putcursor(x, y int) {
	t.buff = fmt.Appendf(t.buff, "\x1b[%v;%vH", y+1, x+1)
}

func (t *unixVT100term) write(b []byte) {
	t.buff = slices.Concat(t.buff, b)
}

func (t *unixVT100term) flush() {
	t.w.Write(t.buff)
	t.buff = []byte{}
}

/*
 * unicode utilities
 */

func fullwidth(r rune) bool {
	fullwidth := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x3000, 0x3000, 1},
			{0xff01, 0xff60, 1},
			{0xffe0, 0xffe6, 1},
		},
	}

	wide := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x1100, 0x115F, 1},
			{0x2329, 0x232A, 1},
			{0x2E80, 0x2FFB, 1},
			{0x3001, 0x303E, 1},
			{0x3041, 0x33FF, 1},
			{0x3400, 0x4DB5, 1},
			{0x4E00, 0x9FBB, 1},
			{0xA000, 0xA4C6, 1},
			{0xAC00, 0xD7A3, 1},
			{0xF900, 0xFAD9, 1},
			{0xFE10, 0xFE19, 1},
			{0xFE30, 0xFE6B, 1},
		},
		R32: []unicode.Range32{
			{0x20000, 0x2A6D6, 1},
			{0x2A6D7, 0x2F7FF, 1},
			{0x2F800, 0x2FA1D, 1},
			{0x2FA1E, 0x2FFFD, 1},
			{0x30000, 0x3FFFD, 1},
		},
	}

	ambiguous := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x00A1, 0x00A1, 1},
			{0x00A4, 0x00A4, 1},
			{0x00A7, 0x00A8, 1},
			{0x00AA, 0x00AA, 1},
			{0x00AD, 0x00AE, 1},
			{0x00B0, 0x00B4, 1},
			{0x00B6, 0x00BA, 1},
			{0x00BC, 0x00BF, 1},
			{0x00C6, 0x00C6, 1},
			{0x00D0, 0x00D0, 1},
			{0x00D7, 0x00D8, 1},
			{0x00DE, 0x00E1, 1},
			{0x00E6, 0x00E6, 1},
			{0x00E8, 0x00EA, 1},
			{0x00EC, 0x00ED, 1},
			{0x00F0, 0x00F0, 1},
			{0x00F2, 0x00F3, 1},
			{0x00F7, 0x00FA, 1},
			{0x00FC, 0x00FC, 1},
			{0x00FE, 0x00FE, 1},
			{0x0101, 0x0101, 1},
			{0x0111, 0x0111, 1},
			{0x0113, 0x0113, 1},
			{0x011B, 0x011B, 1},
			{0x0126, 0x0127, 1},
			{0x012B, 0x012B, 1},
			{0x0131, 0x0133, 1},
			{0x0138, 0x0138, 1},
			{0x013F, 0x0142, 1},
			{0x0144, 0x0144, 1},
			{0x0148, 0x014B, 1},
			{0x014D, 0x014D, 1},
			{0x0152, 0x0153, 1},
			{0x0166, 0x0167, 1},
			{0x016B, 0x016B, 1},
			{0x01CE, 0x01CE, 1},
			{0x01D0, 0x01D0, 1},
			{0x01D2, 0x01D2, 1},
			{0x01D4, 0x01D4, 1},
			{0x01D6, 0x01D6, 1},
			{0x01D8, 0x01D8, 1},
			{0x01DA, 0x01DA, 1},
			{0x01DC, 0x01DC, 1},
			{0x0251, 0x0251, 1},
			{0x0261, 0x0261, 1},
			{0x02C4, 0x02C4, 1},
			{0x02C7, 0x02C7, 1},
			{0x02C9, 0x02CB, 1},
			{0x02CD, 0x02CD, 1},
			{0x02D0, 0x02D0, 1},
			{0x02D8, 0x02DB, 1},
			{0x02DD, 0x02DD, 1},
			{0x02DF, 0x02DF, 1},
			{0x0300, 0x036F, 1},
			{0x0391, 0x03A9, 1},
			{0x03B1, 0x03C1, 1},
			{0x03C3, 0x03C9, 1},
			{0x0401, 0x0401, 1},
			{0x0410, 0x044F, 1},
			{0x0451, 0x0451, 1},
			{0x2010, 0x2010, 1},
			{0x2013, 0x2016, 1},
			{0x2018, 0x2019, 1},
			{0x201C, 0x201D, 1},
			{0x2020, 0x2022, 1},
			{0x2024, 0x2027, 1},
			{0x2030, 0x2030, 1},
			{0x2032, 0x2033, 1},
			{0x2035, 0x2035, 1},
			{0x203B, 0x203B, 1},
			{0x203E, 0x203E, 1},
			{0x2074, 0x2074, 1},
			{0x207F, 0x207F, 1},
			{0x2081, 0x2084, 1},
			{0x20AC, 0x20AC, 1},
			{0x2103, 0x2103, 1},
			{0x2105, 0x2105, 1},
			{0x2109, 0x2109, 1},
			{0x2113, 0x2113, 1},
			{0x2116, 0x2116, 1},
			{0x2121, 0x2122, 1},
			{0x2126, 0x2126, 1},
			{0x212B, 0x212B, 1},
			{0x2153, 0x2154, 1},
			{0x215B, 0x215E, 1},
			{0x2160, 0x216B, 1},
			{0x2170, 0x2179, 1},
			{0x2190, 0x2199, 1},
			{0x21B8, 0x21B9, 1},
			{0x21D2, 0x21D2, 1},
			{0x21D4, 0x21D4, 1},
			{0x21E7, 0x21E7, 1},
			{0x2200, 0x2200, 1},
			{0x2202, 0x2203, 1},
			{0x2207, 0x2208, 1},
			{0x220B, 0x220B, 1},
			{0x220F, 0x220F, 1},
			{0x2211, 0x2211, 1},
			{0x2215, 0x2215, 1},
			{0x221A, 0x221A, 1},
			{0x221D, 0x2220, 1},
			{0x2223, 0x2223, 1},
			{0x2225, 0x2225, 1},
			{0x2227, 0x222C, 1},
			{0x222E, 0x222E, 1},
			{0x2234, 0x2237, 1},
			{0x223C, 0x223D, 1},
			{0x2248, 0x2248, 1},
			{0x224C, 0x224C, 1},
			{0x2252, 0x2252, 1},
			{0x2260, 0x2261, 1},
			{0x2264, 0x2267, 1},
			{0x226A, 0x226B, 1},
			{0x226E, 0x226F, 1},
			{0x2282, 0x2283, 1},
			{0x2286, 0x2287, 1},
			{0x2295, 0x2295, 1},
			{0x2299, 0x2299, 1},
			{0x22A5, 0x22A5, 1},
			{0x22BF, 0x22BF, 1},
			{0x2312, 0x2312, 1},
			{0x2460, 0x24E9, 1},
			{0x24EB, 0x254B, 1},
			{0x2550, 0x2573, 1},
			{0x2580, 0x258F, 1},
			{0x2592, 0x2595, 1},
			{0x25A0, 0x25A1, 1},
			{0x25A3, 0x25A9, 1},
			{0x25B2, 0x25B3, 1},
			{0x25B6, 0x25B7, 1},
			{0x25BC, 0x25BD, 1},
			{0x25C0, 0x25C1, 1},
			{0x25C6, 0x25C8, 1},
			{0x25CB, 0x25CB, 1},
			{0x25CE, 0x25D1, 1},
			{0x25E2, 0x25E5, 1},
			{0x25EF, 0x25EF, 1},
			{0x2605, 0x2606, 1},
			{0x2609, 0x2609, 1},
			{0x260E, 0x260F, 1},
			{0x2614, 0x2615, 1},
			{0x261C, 0x261C, 1},
			{0x261E, 0x261E, 1},
			{0x2640, 0x2640, 1},
			{0x2642, 0x2642, 1},
			{0x2660, 0x2661, 1},
			{0x2663, 0x2665, 1},
			{0x2667, 0x266A, 1},
			{0x266C, 0x266D, 1},
			{0x266F, 0x266F, 1},
			{0x273D, 0x273D, 1},
			{0x2776, 0x277F, 1},
			{0xE000, 0xF8FF, 1},
			{0xFE00, 0xFE0F, 1},
			{0xFFFD, 0xFFFD, 1},
		},
		R32: []unicode.Range32{
			{0xE0100, 0xE01EF, 1},
			{0xF0000, 0xFFFFD, 1},
			{0x100000, 0x10FFFD, 1},
		},
	}

	return unicode.Is(fullwidth, r) || unicode.Is(wide, r) || unicode.Is(ambiguous, r)
}
