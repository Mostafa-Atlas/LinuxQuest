// Package missionsdata embeds the mission definitions (JSON) shipped with LinuxQuest.
// Add a new mission by dropping another JSON file in this directory.
package missionsdata

import "embed"

//go:embed *.json
var FS embed.FS
