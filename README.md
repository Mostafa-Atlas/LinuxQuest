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

## Missions

Missions are JSON files declaring the story, starting files, and objectives.
Objectives are judged by what commands did, not by exact text. Supported kinds:

| Kind | Completed when |
|---|---|
| `ran` | the named `command` ran successfully |
| `listed` | the directory at `path` was listed |
| `read` | the file at `path` was read with `cat` |
| `entered` | `cd` succeeded and the working directory is now `path` |
| `found` | `find` printed `path` as a match |
| `created` | `path` was created by `mkdir`, `touch`, `cp`, or `mv` |
| `removed` | `path` was removed by `rm` or moved away by `mv` |
| `grepped` | `grep` printed a matching line from `path` |

Use `requires` to make an objective wait for others, and `hints` for
progressive hints (vague first, specific last).

## How to play

Thirty missions in three tiers of ten: Beginner, Intermediate, Advanced. Start
at the top: later missions assume earlier skills.

1. Press Enter on the welcome screen.
2. Choose a mission with the menu.
3. Read the objectives, then type commands at the prompt.

Each objective shows when you finish it. Finish all of them to complete the
mission. Stuck? Use `hint` first. Replaying a mission resets its files; XP is
awarded only the first time you ever complete a mission, so replays cannot be
farmed.
