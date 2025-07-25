turtle editor - An old-fashioned cli text editor for unix.

![Image](https://github.com/user-attachments/assets/84280ddc-f619-41ec-a348-3412bbefb21e)

## usage

### build

```shell
go build
```

### install

```shell
go install
```

### start editor

```shell
# run with empty buffer
tt

# open and edit a file
tt test.txt
```

Some behavior can be customized via command line flag.

* `--theme` configures the color theme. Default is "doraemon". Choose from:
  - doraemon
  - nobita
  - shizuka
  - suneo
  - gian

## multi-cursor

In turtle editor, there can be a multiple cursors at once.
When there are several cursors, the input keypress to edit the text is applied to the every cursor.

## keymaps

By default, turtle editor is in normal mode.

### normal mode

For some commands, leading number before command executes the command n-times.
For example, `3j` moves the cursor down by 3 times.

* `i`: enter the insert mode
* `:`: enter the command mode
* `x`: select current line and enter the line-selection mode
* `C`: add cursor below
* `,`: close cursors except the last one
* `h`: move left
* `j`: move down
* `k`: move up
* `l`: move right
* `f <character>`: find and move to the **next** \<character\> on the current line
* `F <character>`: find and move to the **previous** \<character\> on the current line
* `r <character>`: replace current character with \<character\>
* `o`: insert a line **below** the current cursor, then enter insert mode
* `O`: insert a line **above** the current cursor, then enter insert mode
* `d`: delete a character
* `p`: paste current yank
* `<number> G`: move to the \<number\> line
* `gg`: move to the text head
* `ge`: move to the text bottom
* `gh`: move to the current line head
* `gl`: move to the current line tail
* `gs`: move to the current line head where non-space character exists
* `Ctrl-u`: scroll up by half page
* `Ctrl-d`: scroll down by half page
* `Ctrl-w` `h`: move to left window
* `Ctrl-w` `j`: move to below window
* `Ctrl-w` `k`: move to above window
* `Ctrl-w` `l`: move to right window
* `\`: show debug message on the current line

### command mode

Every command is executed after Enter keypress.

* `q`: close the current buffer
* `q!`: close the current buffer even if the unsaved change reminaing
* `w`: save the buffer
* `wq`: save and close the buffer
* `vs filename`: opens a new file in vertically split window
* `hs filename`: opens a new file in horizontally split window

### insert mode

In insert mode, you can edit the text.

* `Esc`: exit insert mode

### line-selection mode

In line-selection mode, you can select the lines.

* `j`: move down to select the line
* `k`: move up to select the line
* `y`: yank selected lines
* `Esc`: discard the current selection and get back to normal mode

## development

### debug

When TURTLE_DEBUG environment variable is set, the debug log via debug() is printed to stderr.
The value is a level of log (bigger is more verbose).

```shell
# run with stderr redirected to another file
TURTLE_DEBUG=1 tt README.md 2>log.txt

# on another terminal, tail it
tail -f log.txt
```
