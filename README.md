turtle editor

## usage

### build

```shell
go build -o tt main.go
```

### run

```shell
# run with empty buffer
./tt

# open and edit a file
./tt test.txt
```

## keymaps

By default, turtle editor is in normal mode.

### normal mode

* `i`: enter insert mode
* `h`: move left
* `j`: move down
* `k`: move up
* `l`: move right
* `o`: insert a line **below** the current cursor, then enter insert mode
* `O`: insert a line **above** the current cursor, then enter insert mode
* `d`: delete a character

### insert mode

In insert mode, you can edit the text.

* `Esc`: exit insert mode
