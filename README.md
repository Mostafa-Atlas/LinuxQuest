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

## Progress

XP, completed missions, commands, hints, and logins are recorded per account.
Progress survives disconnects and restarts.

## Built-in commands

These are handled by the session, not the simulator:

| Command | What it does |
|---|---|
| `help` | List supported commands |
| `mission` | Show the current mission and which objectives are done |
| `missions` | Go back to the mission menu |
| `hint` | Next hint for your current objective. Repeat for more detail |
| `stats` | Your lifetime stats (XP, missions, commands, hints, logins) |
| `top` | XP leaderboard across all players |
| `clear` | Clear the screen (also Ctrl+L) |
| `exit` | End the session and show your summary (also Ctrl+D on an empty line) |

## Accounts

Accounts live in a SQLite database. The first connection for a username creates
the account; afterwards one of its registered keys is required. Only public
keys are stored. The very first account ever created is automatically an admin.

## Terminal keys

Up and Down recall history; Left, Right, Home, and End edit the line. Ctrl+C
clears the current line (or quits when the line is empty). Tab completes the
command or path you are typing. The interface follows your terminal size.

## Connecting

Login is by SSH key only: there are no passwords. Pick any username (letters,
digits, `.` `_` `-`). The first time you connect, that username becomes your
account and your key is registered to it. Connect with:

```bash
ssh -p 2222 -i ~/.ssh/id_ed25519 YOUR_NAME@YOUR_SERVER_IP
```

New players can open `http://YOUR_SERVER_IP:8080` in a browser for step-by-step
key setup, with this server's live fingerprint and port filled in.
