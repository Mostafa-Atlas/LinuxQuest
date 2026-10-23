# LinuxQuest

A game that teaches Linux commands, played over SSH. You connect with a normal `ssh` command, pick a mission, and type commands into a fake shell. The shell is a simulator written in Go, so nothing you type ever touches the real server.

I started it as a two-mission MVP. It now has 30 missions in three tiers, accounts based on SSH keys, saved progress, a leaderboard, and a small web page that helps people set up their keys.

![Mission list](Img/01-missions.png)


## Try online (Demo)

### 1. Make an SSH key (once per device)

**macOS / Linux**

```bash
ssh-keygen -t ed25519 -C "you@linuxquest"
ssh -p 2222 -i ~/.ssh/id_ed25519 YOUR_NAME@16.16.82.170
```
for Windows or Android visit this help page http://16.16.82.170:8080/

## Try it (self-hosted)

You need Docker with the Compose plugin.

```bash
docker compose up -d --build
docker compose ps      # wait for "healthy"
docker compose logs -f linuxquest
```

Then connect from any machine that can reach the server:

```bash
ssh -p 2222 -i ~/.ssh/id_ed25519 YOUR_NAME@YOUR_SERVER_IP
```

There are no passwords, only SSH keys. If you don't have a key yet:

```bash
ssh-keygen -t ed25519 -C "you@linuxquest"
```

Pick any username (letters, digits, `.`, `_`, `-`, up to 32 characters). The first time you connect, that username becomes your account and your key is tied to it. After that, you need a registered key to get back in.

Your SSH client will also ask you to trust the server's host key the first time. That's normal. You can compare the fingerprint with the one on the help page at `http://YOUR_SERVER_IP:8080`, which also has key setup steps for Windows, macOS, Linux, and Android.

To change ports or limits, copy `.env.example` to `.env` and edit it. No code changes needed.

### Without Docker

You need Go 1.22 or newer.

```bash
make test
make run
```

Then `ssh -p 2222 -i ~/.ssh/id_ed25519 you@localhost`. The host key is created at `data/ssh_host_ed25519` and the database at `data/linuxquest.db` on first start. The help page is at `http://localhost:8080`.

## How to play

1. Press Enter on the welcome screen.
2. Pick a mission with the arrow keys, or press a number for the first nine. Start at the top, because later missions assume you learned the earlier stuff.
3. Read the objectives and type commands at the prompt.

Each time you finish an objective, the game tells you. When all of them are done you get a completion screen: Enter or `m` goes back to the menu, `r` replays the mission, `q` quits.

![Playing a mission](Img/02-mission1.png)

The missions come in three tiers of ten:

| Tier | Missions | XP each | What you practice |
|---|---|---|---|
| Beginner | 1 to 10 | 50 (mission 2 gives 75) | `pwd`, `ls`, `cd`, `cat`, `mkdir`, `touch`, `cp`, `mv`, `rm`, `echo` |
| Intermediate | 11 to 20 | 75 | `find`, `grep`, and working across directories |
| Advanced | 21 to 30 | 100 | Multi-step jobs where you search, build, and clean up |

A few things worth knowing:

- Objectives check what your commands did, not what you typed. In mission 2, `cat secrets/clue.txt`, `cat ~/secrets/clue.txt`, and `cd secrets` followed by `cat clue.txt` all count.
- If you're stuck, type `hint`. Repeat it for more detail. Looking things up online is fine too.
- Replaying a mission resets its files. XP is only awarded the first time you finish a mission, so replaying can't be farmed.
- Progress is saved to your account, so you can disconnect and come back.

### Commands

| Command | What it does |
|---|---|
| `help` | List the supported commands |
| `mission` | Show the current mission and which objectives are done |
| `missions` | Back to the mission menu |
| `hint` | Next hint for the current objective |
| `stats` | Your XP, missions, commands, hints, and logins |
| `top` | XP leaderboard |
| `keys` | List your registered SSH keys |
| `keyadd <key>` | Add another key (paste the `.pub` line), handy for a second device |
| `keyrm <fingerprint>` | Remove a key. You can't remove your last one |
| `admin` | Admin console, admins only |
| `clear` | Clear the screen (or Ctrl+L) |
| `exit` | End the session and show a summary (or Ctrl+D on an empty line) |
| `pwd` | Print the current directory |
| `ls [path]` | List a directory. Folders end in `/` |
| `cd [path]` | Change directory. `cd ..` goes up, plain `cd` or `cd ~` goes home |
| `cat <file>...` | Print files |
| `find [path] -name <pattern> [-type f\|d]` | Search by name. `*` and `?` work |
| `mkdir <dir>` | Make a directory (parents included, like `mkdir -p`) |
| `touch <file>` | Make an empty file |
| `rm [-r] [-f] <path>...` | Delete things |
| `cp [-r] <src> <dst>` | Copy |
| `mv <src> <dst>` | Move or rename |
| `echo <text>` | Print text. No redirection yet |
| `grep [-i] [-r] <pattern> [path...]` | Search inside files |

Up and down arrows recall history, Tab completes commands and paths, and Ctrl+C clears the line (or quits if the line is already empty). The interface follows your terminal size, and about 80x24 works fine.

## Configuration

Everything is an environment variable.

| Variable | Default | What it does |
|---|---|---|
| `LINUXQUEST_PORT` | `2222` | SSH port. Compose publishes the same port on the host |
| `LINUXQUEST_HOST` | `0.0.0.0` | Interface the process listens on |
| `LINUXQUEST_HOST_KEY` | `data/ssh_host_ed25519` | Host key path. Generated if it's missing |
| `LINUXQUEST_DB` | `data/linuxquest.db` | SQLite database for accounts and progress |
| `LINUXQUEST_MAX_SESSIONS` | `100` | Concurrent sessions allowed. `0` means no limit |
| `LINUXQUEST_IDLE_TIMEOUT_MIN` | `15` | Disconnect idle players. `0` means never |
| `LINUXQUEST_MAX_SESSION_MIN` | `120` | Longest a session can run. `0` means no cap |
| `LINUXQUEST_ADMINS` | empty | Comma-separated usernames that get admin, like `teacher,ops` |
| `LINUXQUEST_WEB_PORT` | `8080` | Port for the help page. `0` turns it off |
| `LINUXQUEST_BIND` | `0.0.0.0` | Compose only. Use `127.0.0.1` to keep both ports private |

In Docker the host key and the database both live in the `linuxquest-data` volume, so they survive restarts and rebuilds. If you want to use your own host key, put it at the `LINUXQUEST_HOST_KEY` path before the first start.

## Security

The short version: players never get a real shell.

- **No real shell.** Input is split into words and run by a Go simulator over an in-memory filesystem. The code never calls `os/exec` and never reads the host filesystem on a player's behalf.
- **Safe paths.** Paths are resolved against a virtual root, so `..` at `/` stays at `/`. NUL bytes and very long paths are rejected.
- **No shell syntax.** Unquoted `; & | < >` and backticks are rejected, not interpreted. There's no variable expansion or substitution, and control characters are stripped from input.
- **Interactive terminals only.** Connections without a PTY, like `ssh host somecommand`, are refused. No port forwarding, no SFTP.
- **Keys only.** The server stores public keys and nothing else, so there's no password to guess or leak. Failed logins don't say whether the username or the key was wrong.
- **Admins.** The first account ever created becomes an admin, and `LINUXQUEST_ADMINS` can add more. Admins can list, inspect, reset, ban, and promote players from inside the game. Bans and role changes hit online players immediately. You can't ban or demote yourself.
- **Container.** Multi-stage build, distroless image with no shell, non-root (UID 65532), read-only root filesystem, all capabilities dropped, `no-new-privileges`, and memory and process limits. The only mount is the named data volume.
- **Logging.** The server logs connects and disconnects, never keystrokes or commands.
- **Limits.** Max sessions, idle timeout, max session length, 256-character input lines, and bounded scrollback and history.

**Heads up if the server is public:** the first person to register becomes admin, so connect with your own account first, and set `LINUXQUEST_ADMINS`. Anyone with a key can also create an account, by design. If you want a private server, restrict access at the network level.

### Running it on a VPS

- **Keep your admin SSH on port 22** and leave LinuxQuest on 2222. If you want players on 22, move your own sshd first and make sure you can still log in, or you'll lock yourself out.
- **Open only the game port**, for example `sudo ufw allow 2222/tcp`. Cloud providers often have a second firewall (a security group) that needs the same rule. Open 8080 too if you want the help page public.
- **Rate-limit connections at the firewall.** The app caps concurrent sessions but doesn't limit new connections per IP. `ufw limit 2222/tcp` is a quick start, or use nftables or fail2ban.
- **Docker can bypass UFW.** Docker edits iptables directly. Check from outside the machine that only the ports you meant to open are reachable.
- **Private use.** Set `LINUXQUEST_BIND=127.0.0.1` and reach it over a VPN or an SSH tunnel.
- **Back up the volume.** If the host key changes, every player gets a scary "host key changed" warning.

## Adding a mission

Copy any file in `missions/` and edit it. A mission file sets the story, the XP, the starting files, and a list of objectives. The loader checks every file at startup and refuses to run if one is malformed.

Objective kinds:

| Kind | Done when |
|---|---|
| `ran` | the named command ran successfully |
| `listed` | the directory at `path` was listed |
| `read` | the file at `path` was read with `cat` |
| `entered` | `cd` worked and you're now in `path` |
| `found` | `find` printed `path` as a match |
| `created` | `path` was made by `mkdir`, `touch`, `cp`, or `mv` |
| `removed` | `path` was removed by `rm` or moved away by `mv` |
| `grepped` | `grep` printed a matching line from `path` |

Use `requires` to make an objective wait for others, and `hints` for progressive hints (vague first, specific last).

Adding a new *command* is a code change: write a function in `internal/simulator` that records what it did in `Result`.

## Project layout

```text
cmd/linuxquest/        startup, config, graceful shutdown, healthcheck probe
internal/sshserver/    Wish SSH server, key auth and registration
internal/terminal/     Bubble Tea UI: screens, input line, history, styling
internal/session/      per-player state: built-in commands, hints, XP, stats, admin
internal/simulator/    command parser and the pwd/ls/cd/cat/find/... commands
internal/filesystem/   in-memory filesystem and path resolution
internal/missions/     mission loader, validator, objective checks
internal/progress/     session XP and completions
internal/store/        SQLite: accounts, keys, progress, stats, leaderboard
internal/webhelp/      the help web page
missions/              mission definitions (embedded JSON)
tests/                 end-to-end tests over a real SSH connection
```

The simulator knows nothing about missions, and missions only look at what commands did. That's why you can add either without touching the SSH layer.

## Testing

```bash
make test              # go test ./...
go test -race ./...    # needs a C compiler
go vet ./...
```

The tests cover path escapes, every command and its error cases, shell-syntax rejection, mission loading and objective tracking, XP and replays, hints, session isolation, the UI, and full play-throughs over a real SSH connection (including simultaneous sessions, session limits, and host-key persistence).

### A quick manual check

1. Start the server and make a throwaway key: `ssh-keygen -t ed25519 -f /tmp/lq-test -N ""`.
2. Connect with `ssh -p 2222 -i /tmp/lq-test tester@localhost`. The welcome screen should say the account was created.
3. Do mission 1 (`pwd`, `ls`, `cat welcome.txt`) and check you get +50 XP.
4. Do mission 2. Run `hint` first and make sure it doesn't give the answer away. You should end on +75 XP and 125 total.
5. Try to break out: `cat /etc/passwd`, `cd ../../..`, `ls /`, `whoami`, `ls; id`. You should only ever see simulated errors.
6. Connect from a second terminal with a different username and key. Its files and XP should be separate.
7. Reconnect as the first user and run `stats`. Your progress should still be there.
8. Restart the server. The host key fingerprint shouldn't change.

## Troubleshooting

| What you see | Likely cause |
|---|---|
| `Connection refused` | Server isn't running (`docker compose ps`), wrong port, or a firewall is blocking it |
| `Connection timed out` | A firewall or cloud security group is dropping the port |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | The host key changed. Run `ssh-keygen -R "[HOST]:2222"` and reconnect |
| `Requires an active PTY` | You ran `ssh host command`. Use plain `ssh -p 2222 you@host` |
| `Permission denied (publickey)` | Wrong key for that username, or you forgot `-i` |
| `LinuxQuest is full right now` | You hit `LINUXQUEST_MAX_SESSIONS`. Raise it or try again |
| Garbled boxes or colors | Use a UTF-8 terminal with a font that has box-drawing characters |
| Host key permission errors | The volume has to be writable by UID 65532. A fresh named volume handles that |
| Container shows "unhealthy" | Check `docker compose logs linuxquest`. The probe is a TCP connect that expects an `SSH-` banner |

## Known limitations

- There's no password fallback. If you lose every key, the account is gone and admins can't log in as you. Add a second key with `keyadd` while you still can.
- Commands only support a few options on purpose. `ls` takes a path but no flags, and `find` only knows `-name` and `-type`.
- No pipes, redirects, globbing, or variables yet.
- No per-IP rate limiting in the app (see the VPS notes above).
- Backups are just the Docker volume.
- The healthcheck sometimes adds a handshake-EOF line to the logs. It's harmless.

## Screenshot

Finishing a mission:

![Mission complete](Img/03-complete.png)
