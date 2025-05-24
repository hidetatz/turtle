package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

var _debug = os.Getenv("TURTLE_DEBUG") != ""

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

// character represents a single character, such as "a", "1", "#", space, tab, etc.
type character struct {
	r     rune
	tab   bool // true if the character represents Tab.
	nl    bool // true if the character represents new line.
	width int
	str   string
}

func newCharacter(r rune) *character {
	// Raw Tab changes its size dynamically and it's hard to properly display, so
	// Tab is treated as 4 spaces.
	if r == '\t' {
		return &character{tab: true, width: 4, str: "    "}
	}

	// In turtle newline is rendered as a single space.
	if r == '\n' {
		return &character{nl: true, width: 1, str: " "}
	}

	if fullwidth(r) {
		return &character{r: r, width: 2, str: string(r)}
	}

	return &character{r: r, width: 1, str: string(r)}
}

func (c *character) copy() *character {
	return &character{c.r, c.tab, c.nl, c.width, c.str}
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
	return c.str
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
	return &line{buffer: []*character{newCharacter('\n')}}
}

func newline(s string) *line {
	runes := []rune(s)
	buff := make([]*character, len(runes))
	for i := range runes {
		buff[i] = newCharacter(runes[i])
	}
	buff = append(buff, newCharacter('\n'))
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

func (l *line) width() int {
	// todo: can be cached
	x := 0
	for i := range l.length() {
		x += l.buffer[i].width
	}
	return x
}

func (l *line) rightedge() int {
	return l.widthto(l.length() - 1)
}

func (l *line) widthto(idx int) int {
	x := 0
	for i := range idx {
		x += l.buffer[i].width
	}
	return x
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
	tmp := &line{l.buffer[:l.length()-1]}
	return s == tmp.String()
}

func (l *line) copy() *line {
	copy := &line{make([]*character, len(l.buffer))}
	for i := range l.buffer {
		copy.buffer[i] = l.buffer[i].copy()
	}
	return copy
}

func (l *line) clear() {
	l.buffer = []*character{newCharacter('\n')}
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

func (l *line) hlandcut(hl highlighter, from, limit int) string {
	s := l.String()

	// todo: consider containing fullwidth char case
	length := len(s)

	if length < from {
		return ""
	}

	to := min(length, from+limit)

	if lineComment.MatchString(s) || multiLineComment.MatchString(s) {
		return hl.hlcomment(s[from:to])
	}

	return hl.hl(s[from:to])
}

/* highlighter */

var (
	/* golang */

	lineComment      = regexp.MustCompile(`^(.*)//(.*)$`)
	multiLineComment = regexp.MustCompile(`^(.*)/\*(.*)\*/(.*)$`)

	_number = regexp.MustCompile(`(\d+)`)

	_bool       = regexp.MustCompile(`^(.+\s)bool(.*)$`)
	_uint8      = regexp.MustCompile(`^(.+\s)uint8(.*)$`)
	_uint16     = regexp.MustCompile(`^(.+\s)uint16(.*)$`)
	_uint32     = regexp.MustCompile(`^(.+\s)uint32(.*)$`)
	_uint64     = regexp.MustCompile(`^(.+\s)uint64(.*)$`)
	_int8       = regexp.MustCompile(`^(.+\s)int8(.*)$`)
	_int16      = regexp.MustCompile(`^(.+\s)int16(.*)$`)
	_int32      = regexp.MustCompile(`^(.+\s)int32(.*)$`)
	_int64      = regexp.MustCompile(`^(.+\s)int64(.*)$`)
	_float32    = regexp.MustCompile(`^(.+\s)float32(.*)$`)
	_float64    = regexp.MustCompile(`^(.+\s)float64(.*)$`)
	_complex64  = regexp.MustCompile(`^(.+\s)complex64(.*)$`)
	_complex128 = regexp.MustCompile(`^(.+\s)complex128(.*)$`)
	_string     = regexp.MustCompile(`^(.+\s)string(.*)$`)
	_int        = regexp.MustCompile(`^(.+\s)int(.*)$`)
	_uint       = regexp.MustCompile(`^(.+\s)uint(.*)$`)
	_uintptr    = regexp.MustCompile(`^(.+\s)uintptr(.*)$`)
	_byte       = regexp.MustCompile(`^(.+\s)byte(.*)$`)
	_rune       = regexp.MustCompile(`^(.+\s)rune(.*)$`)
	_any        = regexp.MustCompile(`^(.+\s)any(.*)$`)
	_error      = regexp.MustCompile(`^(.+\s)error(.*)$`)
	_comparable = regexp.MustCompile(`^(.+\s)comparable(.*)$`)
	_iota       = regexp.MustCompile(`^(.+\s)iota(.*)$`)
	_nil        = regexp.MustCompile(`^(.+\s)nil(.*)$`)

	_append  = regexp.MustCompile(`^(.*\s)append(\(.*)$`)
	_copy    = regexp.MustCompile(`^(.*\s)copy(\(.*)$`)
	_delete  = regexp.MustCompile(`^(.*\s)delete(\(.*)$`)
	_len     = regexp.MustCompile(`^(.*\s)len(\(.*)$`)
	_cap     = regexp.MustCompile(`^(.*\s)cap(\(.*)$`)
	_make    = regexp.MustCompile(`^(.*\s)make(\(.*)$`)
	_max     = regexp.MustCompile(`^(.*\s)max(\(.*)$`)
	_min     = regexp.MustCompile(`^(.*\s)min(\(.*)$`)
	_new     = regexp.MustCompile(`^(.*\s)new(\(.*)$`)
	_complex = regexp.MustCompile(`^(.*\s)complex(\(.*)$`)
	_real    = regexp.MustCompile(`^(.*\s)real(\(.*)$`)
	_imag    = regexp.MustCompile(`^(.*\s)imag(\(.*)$`)
	_clear   = regexp.MustCompile(`^(.*\s)clear(\(.*)$`)
	_close   = regexp.MustCompile(`^(.*\s)close(\(.*)$`)
	_panic   = regexp.MustCompile(`^(.*\s)panic(\(.*)$`)
	_recover = regexp.MustCompile(`^(.*\s)recover(\(.*)$`)
	_print   = regexp.MustCompile(`^(.*\s)print(\(.*)$`)
	_println = regexp.MustCompile(`^(.*\s)println(\(.*)$`)

	_package     = regexp.MustCompile(`^(\s?)package(\s.*)$`)
	_import      = regexp.MustCompile(`^(\s?)import(\s.*)$`)
	_func        = regexp.MustCompile(`^(.*\s?)func(\s.*)$`)
	_defer       = regexp.MustCompile(`^(\s+)defer(\s.*)$`)
	_return      = regexp.MustCompile(`^(\s+)return(\s.*)$`)
	_for         = regexp.MustCompile(`^(\s+)for(\s.*)$`)
	_range       = regexp.MustCompile(`^(.+\s)range(\s.*)$`)
	_break       = regexp.MustCompile(`^(\s+)break(;?)$`)
	_continue    = regexp.MustCompile(`^(\s+)continue(;?)$`)
	_if          = regexp.MustCompile(`^(\s+)if(\s.*)$`)
	_else        = regexp.MustCompile(`^(.*\s)else(\s.*)$`)
	_var         = regexp.MustCompile(`^(\s*)var(\s.*)$`)
	_const       = regexp.MustCompile(`^(\s*)const(\s.*)$`)
	_switch      = regexp.MustCompile(`^(\s+)switch(\s.*)$`)
	_case        = regexp.MustCompile(`^(\s+)case(\s.*)$`)
	_goto        = regexp.MustCompile(`^(\s+)goto(\s.*)$`)
	_fallthrough = regexp.MustCompile(`^(\s+)fallthrough(;?)$`)
	_default     = regexp.MustCompile(`^(\s+)default(.*)$`)
	_type        = regexp.MustCompile(`^(.*\s)type(\s.*)$`)
	_struct      = regexp.MustCompile(`^(.+\s)struct(\s.*)$`)
	_interface   = regexp.MustCompile(`^(.+\s)interface(\s.*)$`)
	// _map         = regexp.MustCompile(`^(.+)map(.*)$`)
	_select = regexp.MustCompile(`^(\s+)select(\s.*)$`)
	_go     = regexp.MustCompile(`^(\s+)go(\s.*)$`)
	_chan   = regexp.MustCompile(`^(.+\s)chan(\s.*)$`)
	_true   = regexp.MustCompile(`^(.+\s)true(.*)$`)
	_false  = regexp.MustCompile(`^(.+\s)false(.*)$`)
)

type highlighter interface {
	hl(s string) string
	hlcomment(s string) string
}

func colorize(s string, color int) string {
	return fmt.Sprintf("\x1b[38;5;%dm%v\x1b[0m", color, s)
}

// do colorize with 2 surround capture groups
func colorize2(s string, r *regexp.Regexp, repl string, color int) string {
	return r.ReplaceAllString(s, fmt.Sprintf("${1}\x1b[38;5;%dm%v\x1b[0m${2}", color, repl))
}

func replandcolorize(s, repl string, color int) string {
	return strings.ReplaceAll(s, repl, colorize(repl, color))
}

type nophighlighter struct{}

func (h nophighlighter) hlcomment(s string) string { return s }
func (h nophighlighter) hl(s string) string        { return s }

/* golang highlighter */

type golanghighlighter struct{}

func (h golanghighlighter) hlcomment(s string) string {
	commentcolor := 247
	s = lineComment.ReplaceAllString(s, fmt.Sprintf("${1}\x1b[38;5;%dm//${2}\x1b[0m", commentcolor))
	s = multiLineComment.ReplaceAllString(s, fmt.Sprintf("${1}\x1b[38;5;%dm/*${2}*/\x1b[0m${3}", commentcolor))
	return s
}

func (h golanghighlighter) hl(s string) string {
	var (
		colorParen       = 67
		colorSign        = 38
		colorKeyword     = 74
		colorType        = 81
		colorBuiltinFunc = 117
	)
	// colors code:
	// for i in {0..256} ; do printf "\e[38;5;${i}m%3d \e[0m" $i ; [ $((i % 16)) -eq 0 ] && echo ; done

	s = replandcolorize(s, "[", colorParen) // must be at the top to prevent replacing escape seq
	s = replandcolorize(s, "]", colorParen)
	s = replandcolorize(s, "(", colorParen)
	s = replandcolorize(s, ")", colorParen)
	s = replandcolorize(s, "{", colorParen)
	s = replandcolorize(s, "}", colorParen)

	s = replandcolorize(s, "+", colorSign)
	s = replandcolorize(s, "-", colorSign)
	s = replandcolorize(s, "*", colorSign)
	s = replandcolorize(s, "&", colorSign)
	s = replandcolorize(s, "/", colorSign)
	s = replandcolorize(s, "%", colorSign)
	s = replandcolorize(s, "=", colorSign)
	s = replandcolorize(s, ":", colorSign)
	s = replandcolorize(s, "<", colorSign)
	s = replandcolorize(s, ">", colorSign)
	s = replandcolorize(s, "\"", colorSign)
	s = replandcolorize(s, "'", colorSign)
	s = replandcolorize(s, ":=", colorSign)
	s = replandcolorize(s, "==", colorSign)
	s = replandcolorize(s, "<=", colorSign)
	s = replandcolorize(s, ">=", colorSign)
	s = replandcolorize(s, "!=", colorSign)
	s = replandcolorize(s, "+=", colorSign)
	s = replandcolorize(s, "-=", colorSign)
	s = replandcolorize(s, "*=", colorSign)
	s = replandcolorize(s, "/=", colorSign)
	s = replandcolorize(s, "%=", colorSign)
	s = replandcolorize(s, "&&", colorSign)
	s = replandcolorize(s, "||", colorSign)

	s = colorize2(s, _append, "append", colorBuiltinFunc)
	s = colorize2(s, _copy, "copy", colorBuiltinFunc)
	s = colorize2(s, _delete, "delete", colorBuiltinFunc)
	s = colorize2(s, _len, "len", colorBuiltinFunc)
	s = colorize2(s, _cap, "cap", colorBuiltinFunc)
	s = colorize2(s, _make, "make", colorBuiltinFunc)
	s = colorize2(s, _max, "max", colorBuiltinFunc)
	s = colorize2(s, _min, "min", colorBuiltinFunc)
	s = colorize2(s, _new, "new", colorBuiltinFunc)
	s = colorize2(s, _complex, "complex", colorBuiltinFunc)
	s = colorize2(s, _real, "real", colorBuiltinFunc)
	s = colorize2(s, _imag, "imag", colorBuiltinFunc)
	s = colorize2(s, _clear, "clear", colorBuiltinFunc)
	s = colorize2(s, _close, "close", colorBuiltinFunc)
	s = colorize2(s, _panic, "panic", colorBuiltinFunc)
	s = colorize2(s, _recover, "recover", colorBuiltinFunc)
	s = colorize2(s, _print, "print", colorBuiltinFunc)
	s = colorize2(s, _println, "println", colorBuiltinFunc)

	s = colorize2(s, _package, "package", colorKeyword)
	s = colorize2(s, _import, "import", colorKeyword)
	s = colorize2(s, _func, "func", colorKeyword)
	s = colorize2(s, _defer, "defer", colorKeyword)
	s = colorize2(s, _return, "return", colorKeyword)
	s = colorize2(s, _for, "for", colorKeyword)
	s = colorize2(s, _range, "range", colorKeyword)
	s = colorize2(s, _break, "for", colorKeyword)
	s = colorize2(s, _continue, "for", colorKeyword)
	s = colorize2(s, _if, "if", colorKeyword)
	s = colorize2(s, _else, "else", colorKeyword)
	s = colorize2(s, _var, "var", colorKeyword)
	s = colorize2(s, _const, "const", colorKeyword)
	s = colorize2(s, _switch, "switch", colorKeyword)
	s = colorize2(s, _case, "case", colorKeyword)
	s = colorize2(s, _goto, "goto", colorKeyword)
	s = colorize2(s, _fallthrough, "fallthrough", colorKeyword)
	s = colorize2(s, _default, "default", colorKeyword)
	s = colorize2(s, _type, "type", colorKeyword)
	s = colorize2(s, _struct, "struct", colorKeyword)
	s = colorize2(s, _interface, "interface", colorKeyword)
	// s = colorize2(s, _map, "map", colorKeyword)
	s = colorize2(s, _select, "select", colorKeyword)
	s = colorize2(s, _go, "go", colorKeyword)
	s = colorize2(s, _chan, "chan", colorKeyword)
	s = colorize2(s, _iota, "iota", colorKeyword)
	s = colorize2(s, _nil, "nil", colorKeyword)
	s = colorize2(s, _true, "true", colorKeyword)
	s = colorize2(s, _false, "false", colorKeyword)

	s = colorize2(s, _bool, "bool", colorType)
	s = colorize2(s, _uint8, "uint8", colorType)
	s = colorize2(s, _uint16, "uint16", colorType)
	s = colorize2(s, _uint32, "uint32", colorType)
	s = colorize2(s, _uint64, "uint64", colorType)
	s = colorize2(s, _int8, "int8", colorType)
	s = colorize2(s, _int16, "int16", colorType)
	s = colorize2(s, _int32, "int32", colorType)
	s = colorize2(s, _int64, "int64", colorType)
	s = colorize2(s, _float32, "float32", colorType)
	s = colorize2(s, _float64, "float64", colorType)
	s = colorize2(s, _complex64, "complex64", colorType)
	s = colorize2(s, _complex128, "complex128", colorType)
	s = colorize2(s, _string, "string", colorType)
	s = colorize2(s, _int, "int", colorType)
	s = colorize2(s, _uint, "uint", colorType)
	s = colorize2(s, _uintptr, "uintptr", colorType)
	s = colorize2(s, _byte, "byte", colorType)
	s = colorize2(s, _rune, "rune", colorType)
	s = colorize2(s, _any, "any", colorType)
	s = colorize2(s, _error, "error", colorType)
	s = colorize2(s, _comparable, "comparable", colorType)

	return s
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

type screen struct {
	term            *screenterm
	hl              highlighter
	width           int
	height          int
	lines           []*line
	file            file
	linenumberwidth int

	yankedch *character

	// current desired cursor position. might be different with the actual position.
	x int
	y int

	actualx int
	xoffset int
	yoffset int

	scrolled     bool
	changedlines []int
	changemode   func(mode mode)

	dirty bool
}

func newscreen(term terminal, x, y, width, height int, file file, changemode func(mode mode)) *screen {
	s := &screen{
		term:       &screenterm{term: term, width: width, x: x, y: y},
		height:     height,
		width:      width,
		file:       file,
		x:          0,
		y:          0,
		xoffset:    0,
		yoffset:    0,
		lines:      []*line{},
		changemode: changemode,
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		s.lines = append(s.lines, newline(line))
	}

	if len(s.lines) == 0 {
		s.lines = []*line{newemptyline()}
	}

	s.updatelinenumberwidth()

	filename := file.Name()
	switch {
	case strings.HasSuffix(filename, ".go"):
		s.hl = golanghighlighter{}
	default:
		s.hl = nophighlighter{}
	}

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

func (s *screen) curline() *line {
	return s.lines[s.y]
}

func (s *screen) render(first bool) {
	// when the x is too right, set x to the line tail.
	// This must not change s.x because s.x should be kept when moving to another long line.
	x := min(s.x, s.curline().width()-1)

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
			if s.curline().width()-1 < x+xpad && x <= s.xoffset+s.width-(s.linenumberwidth+1)-1 {
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
		padup := s.y - s.yoffset
		paddown := (s.yoffset + s.height - 2) - s.y

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

	displine := func(y int) string {
		line := s.lines[y]
		linenumber := fmt.Sprintf("%v\x1b[38;5;243m%v\x1b[0m", strings.Repeat(" ", s.linenumberwidth-calcdigit(y+1)), y+1)
		return fmt.Sprintf("%v %v", linenumber, line.hlandcut(s.hl, s.xoffset, s.width-1-(s.linenumberwidth+1)))
	}

	/* update texts */
	if scrolled || s.scrolled || first {
		// update all lines
		for i := range s.height - 1 {
			s.term.clearline(i)
			if s.yoffset+i < len(s.lines) {
				fmt.Fprint(s.term, displine(s.yoffset+i))
			}
		}
	} else if len(s.changedlines) != 0 {
		// udpate only changed lines
		slices.Sort(s.changedlines)
		s.changedlines = slices.Compact(s.changedlines)
		for _, l := range s.changedlines {
			// if changed line is not shown on the screen, skip
			if l < s.yoffset || s.height-2 < l-s.yoffset {
				continue
			}

			s.term.clearline(l - s.yoffset)

			if l <= len(s.lines)-1 {
				fmt.Fprint(s.term, displine(l))
			}
		}
	}

	// render status line
	s.term.clearline(s.height - 1)
	fmt.Fprint(s.term, s.statusline().hlandcut(s.hl, 0, s.width-(s.linenumberwidth+1)))

	s.term.putcursor(x-s.xoffset+s.linenumberwidth+1, s.y-s.yoffset)
	s.actualx = x - s.xoffset + s.linenumberwidth + 1
	s.changedlines = []int{}
	s.scrolled = false
}

func (s *screen) handle(mode mode, buff *input, reader *reader) {
	num := 1
	isnum, n := buff.isnumber()
	if mode == normal && isnum {
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

	switch mode {
	case normal:
		switch buff.special {
		case _left:
			s.movecursor(left, num)

		case _down:
			s.movecursor(down, num)

		case _up:
			s.movecursor(up, num)

		case _right:
			s.movecursor(right, num)

		case _ctrl_u:
			move := (s.height - 1) / 2
			s.y = max(0, s.y-move)
			s.yoffset = max(0, s.yoffset-move)
			s.scrolled = true

		case _ctrl_d:
			move := (s.height - 1) / 2
			s.y = min(len(s.lines)-1, s.y+move)
			s.yoffset = s.yoffset + move
			s.scrolled = true

		case _not_special_key:
			switch buff.r {
			case 'd':
				switch {
				case s.xidx() == s.curline().length()-1:
					if s.y+1 < len(s.lines) {
						// if x is at last, it's removing nl so concat current and next line.
						s.joinlines(s.y, s.y+1)
					}

				default:
					s.delchar()
					s.alignx()
				}

			case 'o':
				s.insline(down)
				s.movecursor(down, 1)
				s.x = 0
				s.changemode(insert)

			case 'O':
				s.insline(up)
				s.x = 0
				s.changemode(insert)

			case 'G':
				s.gotoline(num)

			/*
			 * goto mode
			 */
			case 'g':
				input2 := reader.read()
				switch input2.r {
				case 'g':
					s.gototopleft()

				case 'e':
					s.gotobottomleft()

				case 'l':
					s.gotolinebottom()

				case 's':
					// move to (first non space character index on curren line, currentline)
					s.gotolineheadch()

				case 'h':
					// move to (0, currentline)
					s.gotolinehead()

				default:
					// do nothing
				}

			case 'f':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.movetonextch(newCharacter(input2.r))
				}

			case 'F':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.movetoprevch(newCharacter(input2.r))
				}

			case 'r':
				input2 := reader.read()
				if input2.special == _not_special_key {
					s.replacech(newCharacter(input2.r))
				}

			case 'y':
				s.yankch()

			case 'p':
				if s.yankedch != nil {
					s.alignx()
					s.inschars([]*character{s.yankedch})
					s.movecursor(right, 1)
				}

			case 'h':
				s.movecursor(left, num)

			case 'j':
				s.movecursor(down, num)

			case 'k':
				s.movecursor(up, num)

			case 'l':
				s.movecursor(right, num)

			}

		}

	case insert:
		switch buff.special {
		case _left:
			s.movecursor(left, 1)

		case _down:
			s.movecursor(down, 1)

		case _up:
			s.movecursor(up, 1)

		case _right:
			s.movecursor(right, 1)

		case _esc:
			s.changemode(normal)

		case _cr:
			curline := s.curline().copy()
			nextline := s.curline().copy()
			curidx := s.xidx()

			s.clearline()
			s.inscharsat(curline.buffer[:curidx], 0)

			s.insline(down)
			s.movecursor(down, 1)
			s.inscharsat(nextline.buffer[curidx:len(nextline.buffer)-1], 0)
			s.x = 0

		case _bs:
			switch s.xidx() {
			case 0:
				if s.y != 0 {
					// join current and above line
					// next x is right edge on the above line
					nextx := s.lines[s.y-1].rightedge()
					s.joinlines(s.y-1, s.y)
					s.movecursor(up, 1)
					s.x = nextx
				}

			default:
				// just delete the char

				// move cursor before deleting char to prevent
				// the cursor points nowhere after deleting the rightmost char.
				s.movecursor(left, 1)
				s.delcharat(s.xidx() - 1)
			}

		case _tab:
			s.alignx()
			s.inschars([]*character{newCharacter('\t')})
			s.movecursor(right, 1)

		case _not_special_key:
			s.alignx()
			s.inschars([]*character{newCharacter(buff.r)})
			s.movecursor(right, 1)
		}

	default:
		panic(fmt.Sprintf("cannot handle mode %v", mode))
	}
}

func (s *screen) String() string {
	return fmt.Sprintf(
		"{term: %v, width: %v, height: %v, x: %v, y: %v, actualx: %v, xoffset: %v, yoffset: %v, file: %v, lines: %v}",
		s.term, s.width, s.height, s.x, s.y, s.actualx, s.xoffset, s.yoffset, s.file.Name(), len(s.lines),
	)
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

func (s *screen) movecursor(direction direction, cnt int) {
	switch direction {
	case up:
		s.y = max(s.y-cnt, 0)

	case down:
		s.y = min(s.y+cnt, len(s.lines)-1)

	case left:
		nextx := max(0, s.xidx()-cnt)
		s.x = s.curline().widthto(nextx)

	case right:
		nextx := min(s.curline().length()-1, s.xidx()+cnt)
		s.x = s.curline().widthto(nextx)

	default:
		panic("invalid direction is passed")
	}
}

func (s *screen) movetonextch(c *character) {
	line := s.curline()
	for i := s.xidx() + 1; i < line.length(); i++ {
		if line.buffer[i].equal(c) {
			s.x = line.widthto(i)
			break
		}
	}
	// if c is not found, do not move
}

func (s *screen) movetoprevch(c *character) {
	line := s.curline()
	for i := s.xidx() - 1; 0 <= i; i-- {
		if line.buffer[i].equal(c) {
			s.x = line.widthto(i)
			break
		}
	}
	// if c is not found, do not move
}

func (s *screen) gototopleft() {
	s.x, s.y = 0, 0
}

func (s *screen) gotobottomleft() {
	s.x, s.y = 0, len(s.lines)-1
}

func (s *screen) gotolinebottom() {
	s.x = s.curline().width() - 1
}

func (s *screen) gotolineheadch() {
	x := 0
	curline := s.curline()
	for i := range curline.buffer {
		if !curline.buffer[i].isspace() {
			break
		}
		x += curline.buffer[i].width
	}
	s.x = x
}

func (s *screen) gotolinehead() {
	s.x = 0
}

func (s *screen) gotoline(line int) {
	if len(s.lines) < line {
		line = len(s.lines)
	}

	s.y = line - 1
}

// insert a line
func (s *screen) insline(direction direction) {
	switch direction {
	case up:
		s.lines = slices.Insert(s.lines, s.y, newemptyline())
	case down:
		s.lines = slices.Insert(s.lines, s.y+1, newemptyline())
	default:
		panic("invalid direction is passed to addline")
	}

	for i := s.y; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}
	s.dirty = true
	s.updatelinenumberwidth()
}

// insert characters
func (s *screen) inschars(chars []*character) {
	s.inscharsat(chars, s.xidx())
}

func (s *screen) inscharsat(chars []*character, at int) {
	s.curline().inschars(chars, at)
	s.changedlines = append(s.changedlines, s.y)
	s.dirty = true
}

// delete a char on (idx, s.y)
func (s *screen) delcharat(idx int) {
	s.curline().delchar(idx)
	s.changedlines = append(s.changedlines, s.y)
	s.dirty = true
	s.updatelinenumberwidth()
}

// delete current cursor character
func (s *screen) delchar() {
	s.delcharat(s.xidx())
	s.dirty = true
}

func (s *screen) replacech(ch *character) {
	s.curline().replacech(ch, s.xidx())
	s.changedlines = append(s.changedlines, s.y)
	s.dirty = true
}

func (s *screen) yankch() {
	s.yankedch = s.curline().buffer[s.xidx()]
}

func (s *screen) pastech() {
	s.yankedch = s.curline().buffer[s.xidx()]
}

// delete a line
func (s *screen) delline(y int) {
	for i := y; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}
	s.lines = slices.Delete(s.lines, y, y+1)
	s.updatelinenumberwidth()
}

// return x character index from the current cursor position on screen
func (s *screen) xidx() int {
	return s.curline().charidx(s.actualx-s.linenumberwidth-1, s.xoffset)
}

// ensure current s.x is pointing on the correct character position.
// if x is too right after up/down move, fix x position.
// if x is not aligning to the multi length character head, align there.
func (s *screen) alignx() {
	s.x = s.curline().widthto(s.xidx())
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

	s.changedlines = append(s.changedlines, from)
	s.dirty = true
	s.updatelinenumberwidth()
}

func (s *screen) clearline() {
	s.curline().clear()
	s.changedlines = append(s.changedlines, s.y)
}

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
				buf = append(buf, byte(ch.r))
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

func newleafwindow(term terminal, x, y, width, height int, file file, modechange func(mode mode)) *window {
	return &window{
		x:      x,
		y:      y,
		width:  width,
		height: height,
		screen: newscreen(term, x, y, width, height, file, modechange),
	}
}

func (w *window) isroot() bool {
	return w.parent == nil
}

func (w *window) isleaf() bool {
	return len(w.children) == 0
}

func (w *window) split(term terminal, modechange func(mode mode), direction direction, file file) *window {
	// when the given directions is the same with parent window, add new window as sibling of w.
	if w.parent != nil && w.parent.direction == direction {
		return w.parent.inschildafter(w, term, modechange, file)
	}

	// when no parent exists (= w is root) or exists but direction is different,
	// make the leaf window w to inner window, then add new window as child.
	w.toinner(direction)
	return w.inschildafter(w.children[0], term, modechange, file)
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

func (w *window) inschildafter(after *window, term terminal, modechange func(mode mode), file file) *window {
	// insert a child node after $after then do resize.
	newwin := newleafwindow(term, 0, 0, 0, 0, file, modechange)
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
					fmt.Fprintf(term, "|")
				}
			} else {
				for j := range child.width {
					term.putcursor(child.x+j, child.y+child.height)
					fmt.Fprintf(term, "-")
				}

			}
		}
	}
}

func (w *window) actualcursor() (int, int) {
	return w.x + w.screen.actualx, w.y + w.screen.y - w.screen.yoffset
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

func neweditor(term *screenterm) *editor {
	return &editor{term: term}
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

	e.activewin = e.activewin.split(e.term.term, func(mode mode) { e.mode = mode }, direction, file)
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
	/* update command line */
	e.term.clearline(e.height - 1)
	if !e.msg.empty() {
		fmt.Fprint(e.term, e.msg.hlandcut(nophighlighter{}, 0, e.width))
	} else {
		fmt.Fprint(e.term, e.commandline().hlandcut(nophighlighter{}, 0, e.width))
	}

	if e.windowchanged {
		e.rootwin.render(e.term, true)
		e.activewin.screen.render(first)
	} else {
		e.activewin.render(e.term, first)
	}

	if e.mode == command {
		e.term.putcursor(e.cmdx+1, e.height-1)
	}

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
	debug("%v\n", e)
}

func start(term terminal, in io.Reader, file file) {
	fin, err := term.init()
	if err != nil {
		fin()
		panic(err)
	}

	defer fin()
	defer term.refresh()

	term.refresh()

	/*
	 * Prepare editor state
	 */

	height, width, err := term.windowsize()
	if err != nil {
		panic(err)
	}

	e := &editor{
		term:    newscreenterm(term, 0, 0, width),
		height:  height,
		width:   width,
		mode:    normal,
		cmdline: newemptyline(),
		cmdx:    0,
		msg:     newemptyline(),
	}

	e.rootwin = newleafwindow(e.term.term, 0, 0, e.width, e.height-1, file, func(mode mode) { e.mode = mode })
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
				e.cmdline.inschars([]*character{newCharacter(buff.r)}, e.cmdxidx())
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
					e.activewin.screen.handle(e.mode, buff, reader)
				}
			default:
				e.activewin.screen.handle(e.mode, buff, reader)
			}

		case insert:
			switch buff.special {
			case _esc:
				e.changemode(normal)
			default:
				e.activewin.screen.handle(e.mode, buff, reader)
			}

		default:
			panic("unknown mode")
		}

		e.render(false)
		e.debug()
	}

finish:
	e.term.refresh()
	fmt.Fprintf(os.Stdout, "\n")
	e.term.putcursor(0, 0)
}

func main() {
	flag.Parse()
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

	start(&unixVT100term{}, os.Stdin, file)
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
			debug("read: unknown input detected: %v\n", string(dbg))
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

func debug(format string, a ...any) (int, error) {
	if _debug {
		return fmt.Fprintf(os.Stderr, format, a...)
	}
	return 0, nil
}

/*
 * abstract virtual terminal interface
 */

type screenterm struct {
	io.Writer

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

func (st *screenterm) clearline(y int) {
	st.putcursor(0, y)
	st.term.clearline(st.width)
	st.putcursor(0, y)
}

func (st *screenterm) putcursor(x, y int) {
	st.term.putcursor(st.x+x, st.y+y)
}

func (st *screenterm) Write(p []byte) (int, error) {
	return st.term.Write(p)
}

/*
 * generic terminal
 */

type terminal interface {
	io.Writer

	init() (func(), error)
	windowsize() (int, int, error)
	refresh()
	clearline(width int)
	putcursor(x, y int)
}

type unixVT100term struct{}

func (t *unixVT100term) init() (func(), error) {
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
	fmt.Fprint(t, "\x1b[2J")
}

func (t *unixVT100term) clearline(width int) {
	t.Write([]byte(strings.Repeat(" ", width)))
}

func (t *unixVT100term) putcursor(x, y int) {
	fmt.Fprint(t, fmt.Sprintf("\x1b[%v;%vH", y+1, x+1))
}

func (t *unixVT100term) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
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
