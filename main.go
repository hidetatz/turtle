package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

var _debug bool

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

func (l *line) cut(from, limit int) string {
	s := l.String()
	// todo: consider containing fullwidth char case
	length := len(s)

	if length < from {
		return ""
	}

	to := min(length, from+limit)

	return s[from:to]
}

/*
 * screen
 */

type screen struct {
	term   terminal
	height int
	width  int
	mode   mode
	lines  []*line

	cmdline *line
	cmdx    int

	errmsg *line

	// current desired cursor position. might be different with the actual position.
	x int
	y int

	// terminal state
	actualx int
	xoffset int
	yoffset int

	modechanged  bool
	changedlines []int
}

func (s *screen) statlineidx() int {
	// in turtle, the bottom line is command line,
	// the second from bottom line is status line.
	// s.height is set as (actual_height - 2) to preserve these lines.
	return s.height
}

func (s *screen) cmdlineidx() int {
	return s.height + 1
}

func (s *screen) changemode(mode mode) {
	s.mode = mode
	s.modechanged = true
}

func (s *screen) statusline() *line {
	return newline(fmt.Sprintf("mode: %v", s.mode))
}

func (s *screen) commandline() *line {
	if s.mode != command {
		return newemptyline()
	}

	return newline(fmt.Sprintf(":%v", s.cmdline))
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
		if s.xoffset+s.width-1 < x+xpad {
			if s.curline().width()-1 < x+xpad && x <= s.xoffset+s.width-1 {
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
		// too high, scroll up
		if s.y-ypad < s.yoffset {
			if s.y < ypad {
				// too high but no enough space above
				return 0
			}
			return up
		}

		// too low, scroll down
		if s.yoffset+s.height-1 < s.y+ypad {
			if len(s.lines)-1 < s.y+ypad {
				// too low but no enough space below
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

	/* update status line */
	if s.modechanged {
		s.term.putcursor(0, s.statlineidx())
		s.term.clearline()
		fmt.Fprint(s.term, s.statusline().cut(0, s.width))
	}

	/* update command line */
	s.term.putcursor(0, s.cmdlineidx())
	if !s.errmsg.empty() {
		s.term.clearline()
		fmt.Fprint(s.term, s.errmsg.cut(0, s.width))
	} else {
		s.term.clearline()
		fmt.Fprint(s.term, s.commandline().cut(0, s.width))
	}

	/* update texts */
	if scrolled || first {
		// update all lines
		for i := range s.height {
			s.term.putcursor(0, i)
			s.term.clearline()
			if s.yoffset+i < len(s.lines) {
				line := s.lines[s.yoffset+i]
				fmt.Fprint(s.term, line.cut(s.xoffset, s.width))
			}
		}
	} else if len(s.changedlines) != 0 {
		// udpate only changed lines
		slices.Sort(s.changedlines)
		s.changedlines = slices.Compact(s.changedlines)
		for _, l := range s.changedlines {
			// if changed line is not shown on the screen, skip
			if l < s.yoffset || s.height-1 < l-s.yoffset {
				continue
			}

			s.term.putcursor(0, l-s.yoffset)
			s.term.clearline()

			if l <= len(s.lines)-1 {
				line := s.lines[l].cut(s.xoffset, s.width)
				fmt.Fprint(s.term, line)
			}
		}
	}

	if s.mode == command {
		s.term.putcursor(s.cmdx+1, s.cmdlineidx())
	} else {
		s.term.putcursor(x-s.xoffset, s.y-s.yoffset)
	}
	s.actualx = x - s.xoffset
	s.modechanged = false
	s.changedlines = []int{}
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (s *screen) movecmdcursor(direction direction) {
	switch direction {
	case left:
		nextx := max(0, s.cmdxidx()-1)
		s.cmdx = s.cmdline.widthto(nextx)
	case right:
		nextx := min(s.cmdline.length()-1, s.cmdxidx()+1)
		s.cmdx = s.cmdline.widthto(nextx)
	default:
		panic("invalid direction is passed")
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
}

// insert characters
func (s *screen) inschars(chars []*character) {
	s.inscharsat(chars, s.xidx())
}

func (s *screen) inscharsat(chars []*character, at int) {
	s.curline().inschars(chars, at)
	s.changedlines = append(s.changedlines, s.y)
}

// delete a char on (idx, s.y)
func (s *screen) delcharat(idx int) {
	s.curline().delchar(idx)
	s.changedlines = append(s.changedlines, s.y)
}

// delete current cursor character
func (s *screen) delchar() {
	s.delcharat(s.xidx())
}

// delete a line
func (s *screen) delline(y int) {
	for i := y; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}
	s.lines = slices.Delete(s.lines, y, y+1)
}

// return x character index from the current cursor position on screen
func (s *screen) xidx() int {
	return s.curline().charidx(s.actualx, s.xoffset)
}

func (s *screen) cmdxidx() int {
	return s.cmdline.charidx(s.cmdx, 0)
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
}

func (s *screen) clearline() {
	s.curline().clear()
	s.changedlines = append(s.changedlines, s.y)
}

func (s *screen) debug(msg ...string) {
	debug("msg: %v, height: %v, width: %v, mode: %v, cmdx: %v, cmdline: %v, x: %v, y: %v, lines: %v, curlinelen: %v, curlinewidth: %v, xoffset: %v, yoffset: %v\n",
		msg, s.height, s.width, s.mode, s.cmdx, s.cmdline, s.x, s.y, len(s.lines), s.curline().length(), s.curline().width(), s.xoffset, s.yoffset)
}

func main() {
	dbg := os.Getenv("TURTLE_DEBUG")
	if dbg != "" {
		_debug = true
	}

	flag.Parse()
	args := flag.Args()

	var r io.Reader

	switch len(args) {
	case 0:
		r = strings.NewReader("")

	case 1:
		filename := args[0]
		f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		r = f

	default:
		fmt.Println("more than 2 args are passed")
		return
	}

	term := &unixVT100term{}
	editor(term, r, os.Stdin)
}

func editor(term terminal, text io.Reader, r io.Reader) {
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

	row, col, err := term.windowsize()
	if err != nil {
		panic(err)
	}

	s := &screen{
		term:        term,
		height:      row - 2,
		width:       col,
		mode:        normal,
		cmdline:     newcommandline(),
		cmdx:        0,
		errmsg:      newemptyline(),
		x:           0,
		y:           0,
		xoffset:     0,
		yoffset:     0,
		modechanged: true,
		lines:       []*line{},
	}

	scanner := bufio.NewScanner(text)
	for scanner.Scan() {
		line := scanner.Text()
		s.lines = append(s.lines, newline(line))
	}

	if len(s.lines) == 0 {
		s.lines = []*line{newemptyline()}
	}

	s.render(true)

	/*
	 * start editor main routine
	 */

	reader := bufio.NewReader(r)

	_read := func() *input {
		return read(reader)
	}

	for {
		// reset error message
		// this keeps showing the error message just until the next input
		s.errmsg = newemptyline()

		input := _read()

		switch s.mode {
		case command:
			switch input.special {
			case _left:
				s.movecmdcursor(left)

			case _right:
				s.movecmdcursor(right)

			case _esc:
				s.cmdline = newcommandline()
				s.changemode(normal)

			case _bs:
				s.movecmdcursor(left)
				s.cmdline.delchar(s.cmdxidx())

			case _cr:
				switch {
				case s.cmdline.equal("q"):
					s.cmdline = newcommandline()
					s.cmdx = 0
					goto finish

				default:
					s.cmdline = newcommandline()
					s.cmdx = 0
					s.errmsg = newline("unknown command!")
					s.changemode(normal)
				}

			case _not_special_key:
				s.cmdline.inschars([]*character{newCharacter(input.r)}, s.cmdxidx())
				s.movecmdcursor(right)
			}

		case normal:
			switch input.special {
			case _left:
				s.movecursor(left, 1)

			case _down:
				s.movecursor(down, 1)

			case _up:
				s.movecursor(up, 1)

			case _right:
				s.movecursor(right, 1)

			case _not_special_key:
				switch input.r {
				case ':':
					s.changemode(command)

				case 'i':
					s.changemode(insert)

				case 'd':
					switch {
					case s.xidx() == s.curline().length()-1:
						// if x is at last, it's removing nl so concat current and next line.
						s.joinlines(s.y, s.y+1)

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

				/*
				 * goto mode
				 */
				case 'g':
					// input := read()
					// switch

				case 'h':
					s.movecursor(left, 1)

				case 'j':
					s.movecursor(down, 1)

				case 'k':
					s.movecursor(up, 1)

				case 'l':
					s.movecursor(right, 1)
				}

			}

		case insert:
			switch input.special {
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

			case _not_special_key:
				s.alignx()
				s.inschars([]*character{newCharacter(input.r)})
				s.movecursor(right, 1)
			}

		default:
			panic("unknown mode")
		}

		s.render(false)

		if _debug {
			s.debug()
		}
	}

finish:
	term.refresh()
	fmt.Fprintf(os.Stdout, "\n")
	term.putcursor(0, 0)
}

/*
 * keys
 */

type input struct {
	r       rune
	special key
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
	case _del:
		return "Delete"
	case _up:
		return "↑"
	case _down:
		return "↓"
	case _right:
		return "→"
	case _left:
		return "←"
	case _home:
		return "Home"
	case _end:
		return "End"
	case _insert:
		return "Insert"
	case _delete:
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
	_del

	// arrow
	_up
	_down
	_right
	_left

	_home
	_end
	_insert
	_delete
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

func read(reader *bufio.Reader) (i *input) {
	var dbg []byte

	defer func() {
		if i.special == _unknown {
			debug("read: unknown input detected: %v\n", string(dbg))
		}
	}()

	first, err := reader.ReadByte()
	if err != nil {
		panic(err)
	}

	dbg = append(dbg, first)

	buffered := reader.Buffered()

	if first != 0x1b {
		buf := make([]byte, buffered)
		if buffered != 0 {
			_, err := reader.Read(buf)
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
	n, err := reader.Read(buf)
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
				return &input{special: _delete}
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
	return fmt.Fprintf(os.Stderr, format, a...)
}

/*
 * terminal
 */

func ctrl(input byte) rune {
	return rune(input & 0x1f)
}

type terminal interface {
	io.Writer

	init() (func(), error)
	windowsize() (int, int, error)
	refresh()
	clearline()
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

func (t *unixVT100term) clearline() {
	fmt.Fprint(t, "\x1b[K")
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
