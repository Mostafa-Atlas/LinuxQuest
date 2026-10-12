# LinuxQuest

LinuxQuest is a terminal game that teaches Linux commands. Players connect over
SSH, pick a mission, and type commands into a simulated Linux shell. Nothing
they type ever reaches the real server.

Module `linuxquest`, Go 1.22.

## Virtual filesystem

The game runs on an in-memory filesystem. Every path is normalised lexically
against a virtual root, so `..` at `/` stays at `/`. NUL bytes and over-long
paths are rejected.
