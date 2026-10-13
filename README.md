# LinuxQuest

LinuxQuest is a terminal game that teaches Linux commands. Players connect over
SSH, pick a mission, and type commands into a simulated Linux shell. Nothing
they type ever reaches the real server.

Module `linuxquest`, Go 1.22.

## Virtual filesystem

The game runs on an in-memory filesystem. Every path is normalised lexically
against a virtual root, so `..` at `/` stays at `/`. NUL bytes and over-long
paths are rejected.

## Input

Unquoted `; & | < >` and backticks are rejected rather than interpreted. There
is no variable expansion, globbing, or substitution.

## Commands

Player input is parsed into words and run by a Go simulator over the virtual
filesystem. The code never calls `os/exec`, never invokes a shell, and never
touches the host filesystem.

| Command | What it does |
|---|---|
| `pwd` | Print the current directory |
| `ls [path]` | List a directory (folders end in `/`) |
| `cd [path]` | Change directory. `cd ..` goes up, `cd` or `cd ~` goes home |
| `cat <file>...` | Print files |
| `find [path] -name <pattern> [-type f\|d]` | Search by name; `*` and `?` work inside quotes or not |
| `mkdir <dir>` | Create a directory (parents included, like `mkdir -p`) |
| `touch <file>` | Create an empty file |
| `rm [-r] [-f] <path>...` | Delete (`-r` for directories, `-f` ignores missing files) |
| `cp [-r] <src> <dst>` | Copy (`-r` for directories) |
| `mv <src> <dst>` | Move or rename |
| `echo <text>` | Print text (no redirection yet) |
| `grep [-i] [-r] <pattern> [path...]` | Search file contents (`-r` descends into directories) |
