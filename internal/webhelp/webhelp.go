// Package webhelp serves the LinuxQuest login-help page: how to create an SSH
// key on any OS and connect with it. It is plain static HTML with the live
// server fingerprint and port filled in, so players can verify the host key
// before trusting it. No external assets, so it works offline.
package webhelp

import (
	"fmt"
	"net"
	"net/http"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

// Server renders the help page.
type Server struct {
	hostKeyPath string
	sshPort     string
}

// New returns a help-page server for the given host key file and SSH port.
func New(hostKeyPath, sshPort string) *Server {
	return &Server{hostKeyPath: hostKeyPath, sshPort: sshPort}
}

// Handler serves the page.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.page)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// fingerprint reads the server's host key and returns its SHA256 fingerprint,
// or "" when the key is not readable yet (first boot generates it).
func (s *Server) fingerprint() string {
	raw, err := os.ReadFile(s.hostKeyPath)
	if err != nil {
		return ""
	}
	signer, err := gossh.ParsePrivateKey(raw)
	if err != nil {
		return ""
	}
	return gossh.FingerprintSHA256(signer.PublicKey())
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	fp := s.fingerprint()
	fpBlock := `<p class="warn">The server is still starting its SSH key. Reload this page in a few seconds.</p>`
	if fp != "" {
		fpBlock = fmt.Sprintf(`<p>When you first connect, your SSH client shows the server fingerprint. It must match <em>exactly</em>:</p>
<pre>%s</pre>
<p>If it does not match, stop and ask the server owner. Never accept a changed fingerprint blindly.</p>`, fp)
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = "YOUR_SERVER_IP"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, pageHTML, host, s.sshPort, fpBlock, s.sshPort)
}

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LinuxQuest — connect with your SSH key</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:760px;margin:2rem auto;padding:0 1rem;line-height:1.55;color:#1c1c1c}
h1{font-size:1.7rem}h2{margin-top:2rem;font-size:1.25rem}
pre{background:#f1f3f5;border:1px solid #dde1e6;border-radius:8px;padding:.8rem;overflow-x:auto}
code{background:#f1f3f5;padding:.1rem .35rem;border-radius:4px}
.warn{background:#fff3cd;border:1px solid #e6c200;border-radius:8px;padding:.6rem .9rem}
.step{border-left:4px solid #2f9e44;padding-left:1rem;margin:1.2rem 0}
footer{margin-top:3rem;color:#666;font-size:.9rem}
</style>
</head>
<body>
<h1>Play LinuxQuest with your SSH key</h1>
<p>LinuxQuest logs you in with an <strong>SSH key only — no passwords</strong>.
The first time you connect, your username becomes your account and your key is
registered to it. From then on, that key <em>is</em> your login.</p>

<div class="step">
<h2>1. Make a key (once per device)</h2>
<h3>Windows, macOS or Linux</h3>
<p>Open PowerShell (Windows) or a terminal and run:</p>
<pre>ssh-keygen -t ed25519 -C "you@linuxquest"</pre>
<p>Press Enter three times to accept the defaults (no passphrase is fine for a game key).</p>
<h3>Android (Termius)</h3>
<p>Termius &rarr; Keychain &rarr; <code>+</code> &rarr; Create a new key &rarr; ED25519, then attach it to the host you add in step 2.</p>
</div>

<div class="step">
<h2>2. Connect</h2>
<pre>ssh -p %[2]s -i ~/.ssh/id_ed25519 YOUR_NAME@%[1]s</pre>
<p>Replace <code>YOUR_NAME</code> with the username you want (letters, digits, <code>.</code> <code>_</code> <code>-</code>).
Windows users: use <code>%%USERPROFILE%%\.ssh\id_ed25519</code> after <code>-i</code>.
The very first account ever created becomes an <strong>admin</strong>.</p>
</div>

<div class="step">
<h2>3. Check the fingerprint</h2>
%[3]s
</div>

<div class="step">
<h2>4. Second device? Add its key from inside the game</h2>
<p>On your new device, make a key as in step 1, then show its <em>public</em> part:</p>
<pre>cat ~/.ssh/id_ed25519.pub</pre>
<p>In the game (logged in with your first device), run:</p>
<pre>keys
keyadd ssh-ed25519 AAAA... your-comment</pre>
<p>Manage them any time with <code>keys</code>, <code>keyadd</code> and <code>keyrm</code>.</p>
</div>

<div class="step">
<h2>Stuck in a mission?</h2>
<p>Use the in-game <code>hint</code> command first. Looking up Linux commands online is also fine. Reading docs is part of the job, and the missions test what you can do, not what you memorized.</p>
</div>

<div class="step">
<h2>Troubleshooting</h2>
<p><strong>Permission denied (publickey).</strong> Wrong key, or the username belongs to a different key. Check <code>ssh -p %[4]s -v</code> output for "Offering public key".</p>
<p><strong>WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED.</strong> The server key changed. Only proceed if the owner announced it.</p>
<p><strong>Connection refused / timed out.</strong> Wrong host, port, or a firewall in the way. The game port is <code>%[4]s</code>.</p>
</div>

<footer>LinuxQuest — learn Linux by doing, in a safe simulated terminal.</footer>
</body>
</html>`
