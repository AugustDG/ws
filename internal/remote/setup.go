package remote

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AugustDG/ws/internal/tmux"
)

// Shell runs scripts on a host over one multiplexed ssh connection, so a
// setup authenticates once however many steps it takes.
type Shell struct {
	SSH        string // ssh binary; "ssh" when empty
	Host       string
	ControlDir string // where the control socket lives
}

func (s *Shell) command(tty bool, script string) *exec.Cmd {
	bin := s.SSH
	if bin == "" {
		bin = "ssh"
	}
	flag := "-T"
	if tty {
		flag = "-t"
	}
	return exec.Command(bin, flag,
		"-o", "ControlMaster=auto",
		"-o", "ControlPath="+filepath.Join(s.ControlDir, "%C"),
		"-o", "ControlPersist=60",
		s.Host, "exec sh -c "+tmux.Quote(script))
}

// Output runs script and returns its stdout.
func (s *Shell) Output(script string) (string, error) {
	cmd := s.command(false, script)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// Pipe runs script with stdin as its input.
func (s *Shell) Pipe(script string, stdin io.Reader) error {
	cmd := s.command(false, script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, os.Stderr, os.Stderr
	return cmd.Run()
}

// Interactive runs script on a terminal, so sudo can ask for a password.
func (s *Shell) Interactive(script string) error {
	cmd := s.command(true, script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Close stops the shared connection.
func (s *Shell) Close() {
	bin := s.SSH
	if bin == "" {
		bin = "ssh"
	}
	_ = exec.Command(bin, "-O", "exit", "-o", "ControlPath="+filepath.Join(s.ControlDir, "%C"), s.Host).Run()
}

// Probe is what setup learns about a host in one round trip.
type Probe struct {
	OS, Arch     string // as GOOS/GOARCH: linux, amd64
	Root         bool
	Tmux         string // `tmux -V` output; empty when missing
	WSHash       string // sha256 of ~/.local/bin/ws; empty when missing
	PkgManager   string
	Sudo, Tic    bool
	MissingTerms []string
	TmuxStamp    string // .ws-sync of ~/.config/tmux
	WSStamp      string // .ws-sync of ~/.config/ws
	TmuxConfig   bool   // ~/.config/tmux or ~/.tmux.conf exists
	WSConfig     bool   // ~/.config/ws has config.yaml or layouts
}

// PkgManagers are the package managers setup can install tmux with, in
// the order they're looked for.
var PkgManagers = []string{"apt-get", "dnf", "yum", "apk", "pacman", "zypper", "brew"}

// ProbeScript prints key=value lines for ParseProbe. terms are the
// terminfo entries to check for.
func ProbeScript(terms []string) string {
	q := make([]string, len(terms))
	for i, t := range terms {
		q[i] = tmux.Quote(t)
	}
	return strings.Join([]string{
		`PATH="$HOME/.local/bin:$PATH"`,
		`echo "os=$(uname -s)"; echo "arch=$(uname -m)"; echo "uid=$(id -u)"`,
		`command -v tmux >/dev/null 2>&1 && echo "tmux=$(tmux -V)"`,
		`f="$HOME/.local/bin/ws"; [ -f "$f" ] && echo "ws=$( (sha256sum "$f" 2>/dev/null || shasum -a 256 "$f") | cut -d' ' -f1)"`,
		`for pm in ` + strings.Join(PkgManagers, " ") + `; do command -v $pm >/dev/null 2>&1 && { echo "pm=$pm"; break; }; done`,
		`command -v sudo >/dev/null 2>&1 && echo sudo=1`,
		`command -v tic >/dev/null 2>&1 && echo tic=1`,
		`for t in ` + strings.Join(q, " ") + `; do infocmp "$t" >/dev/null 2>&1 || echo "noterm=$t"; done`,
		`[ -f "$HOME/.config/tmux/` + StampFile + `" ] && echo "tmux_stamp=$(cat "$HOME/.config/tmux/` + StampFile + `")"`,
		`[ -f "$HOME/.config/ws/` + StampFile + `" ] && echo "ws_stamp=$(cat "$HOME/.config/ws/` + StampFile + `")"`,
		`{ [ -e "$HOME/.config/tmux" ] || [ -e "$HOME/.tmux.conf" ]; } && echo tmux_config=1`,
		`{ [ -e "$HOME/.config/ws/config.yaml" ] || [ -e "$HOME/.config/ws/layouts" ]; } && echo ws_config=1`,
		`true`,
	}, "\n")
}

// ParseProbe reads ProbeScript's output.
func ParseProbe(out string) (Probe, error) {
	var p Probe
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "os":
			p.OS = strings.ToLower(v)
		case "arch":
			p.Arch = goArch(v)
		case "uid":
			p.Root = v == "0"
		case "tmux":
			p.Tmux = v
		case "ws":
			p.WSHash = v
		case "pm":
			p.PkgManager = v
		case "sudo":
			p.Sudo = true
		case "tic":
			p.Tic = true
		case "noterm":
			p.MissingTerms = append(p.MissingTerms, v)
		case "tmux_stamp":
			p.TmuxStamp = v
		case "ws_stamp":
			p.WSStamp = v
		case "tmux_config":
			p.TmuxConfig = true
		case "ws_config":
			p.WSConfig = true
		}
	}
	if p.OS == "" || p.Arch == "" {
		return p, fmt.Errorf("unexpected probe output: %q", out)
	}
	return p, nil
}

func goArch(uname string) string {
	switch uname {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return uname
}

// Supported reports whether ws publishes a binary for the host.
func (p Probe) Supported() bool {
	return (p.OS == "linux" || p.OS == "darwin") && (p.Arch == "amd64" || p.Arch == "arm64")
}

// Asset is the release binary for the host.
func (p Probe) Asset() string { return "ws-" + p.OS + "-" + p.Arch }

// Local is what setup would send: hashes of the ws binary and the config
// bundles (empty when there's nothing to send) and the terminfo entries
// available here.
type Local struct {
	WSHash, TmuxHash, WSConfHash string
	Terms                        []string
}

// Step is one line of a plan.
type Step struct {
	Name string
	Do   bool   // there's work to do
	Fail bool   // it needs doing but can't be done
	Note string // what happens, or why not
}

// Plan is what setup does to a host, in order.
type Plan struct {
	Tmux, WS, Terminfo, TmuxConf, WSConf Step
	TmuxInstall                          string   // command that installs tmux
	Terms                                []string // terminfo entries to send
}

// Steps lists the plan's steps in the order they run.
func (p Plan) Steps() []Step { return []Step{p.Tmux, p.WS, p.Terminfo, p.TmuxConf, p.WSConf} }

// NewPlan works out what to change on a host. Config the host already
// has, and that setup didn't write, is only replaced with force.
func NewPlan(p Probe, l Local, force bool) Plan {
	plan := Plan{
		Tmux:     Step{Name: "tmux", Note: p.Tmux},
		WS:       Step{Name: "ws"},
		Terminfo: Step{Name: "terminfo", Note: "ok"},
	}

	if p.Tmux == "" {
		cmd, err := InstallTmux(p)
		plan.Tmux = Step{Name: "tmux", Do: err == nil, Fail: err != nil, Note: "install: " + cmd}
		if err != nil {
			plan.Tmux.Note = err.Error()
		}
		plan.TmuxInstall = cmd
	}

	switch {
	case !p.Supported():
		plan.WS = Step{Name: "ws", Fail: true, Note: fmt.Sprintf("no ws build for %s/%s", p.OS, p.Arch)}
	case p.WSHash == "":
		plan.WS = Step{Name: "ws", Do: true, Note: "install to ~/.local/bin"}
	case p.WSHash != l.WSHash:
		plan.WS = Step{Name: "ws", Do: true, Note: "update"}
	default:
		plan.WS.Note = "up to date"
	}

	for _, t := range p.MissingTerms {
		for _, have := range l.Terms {
			if t == have {
				plan.Terms = append(plan.Terms, t)
			}
		}
	}
	switch {
	case len(plan.Terms) > 0 && !p.Tic:
		plan.Terminfo = Step{Name: "terminfo", Fail: true, Note: "missing " + strings.Join(plan.Terms, ", ") + ", and no tic on the host"}
		plan.Terms = nil
	case len(plan.Terms) > 0:
		plan.Terminfo = Step{Name: "terminfo", Do: true, Note: "install " + strings.Join(plan.Terms, ", ")}
	}

	plan.TmuxConf = syncStep("tmux config", l.TmuxHash, p.TmuxStamp, p.TmuxConfig, force)
	plan.WSConf = syncStep("ws config", l.WSConfHash, p.WSStamp, p.WSConfig, force)
	return plan
}

func syncStep(name, local, stamp string, present, force bool) Step {
	s := Step{Name: name}
	switch {
	case local == "":
		s.Note = "nothing to send"
	case stamp == local:
		s.Note = "up to date"
	case stamp != "":
		s.Do, s.Note = true, "update"
	case present && !force:
		s.Note = "skipped: the host has its own (--force replaces it, keeping a .ws-bak copy)"
	case present:
		s.Do, s.Note = true, "replace, keeping a .ws-bak copy"
	default:
		s.Do, s.Note = true, "install"
	}
	return s
}

// InstallTmux is the command that installs tmux on the host, run on a
// terminal so sudo can prompt.
func InstallTmux(p Probe) (string, error) {
	cmds := map[string][]string{
		"apt-get": {"apt-get update -qq", "env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq tmux"},
		"dnf":     {"dnf install -y tmux"},
		"yum":     {"yum install -y tmux"},
		"apk":     {"apk add tmux"},
		"pacman":  {"pacman -S --noconfirm tmux"},
		"zypper":  {"zypper -n install tmux"},
		"brew":    {"brew install tmux"},
	}
	steps, ok := cmds[p.PkgManager]
	if !ok {
		return "", fmt.Errorf("no supported package manager; install tmux yourself")
	}
	// sudo drops variables set on its command line under the default
	// env_reset, hence `env` for apt-get.
	prefix := ""
	if p.PkgManager != "brew" && !p.Root {
		if !p.Sudo {
			return "", fmt.Errorf("not root and no sudo; install tmux yourself")
		}
		prefix = "sudo "
	}
	for i, s := range steps {
		steps[i] = prefix + s
	}
	return strings.Join(steps, " && "), nil
}

// InstallWSScript reads the ws binary from stdin into ~/.local/bin.
const InstallWSScript = `set -e
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/.ws.new"
chmod 755 "$HOME/.local/bin/.ws.new"
mv -f "$HOME/.local/bin/.ws.new" "$HOME/.local/bin/ws"`

// TerminfoScript compiles terminfo sources from stdin into ~/.terminfo
// (or the system database when root).
const TerminfoScript = `set -e
t=$(mktemp)
trap 'rm -f "$t"' EXIT
cat > "$t"
tic -x "$t"`

// SyncTmuxScript replaces ~/.config/tmux with the tar on stdin. A dir
// setup didn't write, and ~/.tmux.conf (which tmux would read instead),
// are kept as .ws-bak. A running tmux server reloads the config.
func SyncTmuxScript(hash string) string {
	return `set -e
d="$HOME/.config/tmux"; n="$HOME/.config/.tmux.ws-new"
mkdir -p "$HOME/.config"; rm -rf "$n"; mkdir -p "$n"
tar -xzf - -C "$n"
printf %s ` + tmux.Quote(hash) + ` > "$n/` + StampFile + `"
if [ -e "$d" ] && [ ! -f "$d/` + StampFile + `" ]; then rm -rf "$d.ws-bak"; mv "$d" "$d.ws-bak"; else rm -rf "$d"; fi
if [ -e "$HOME/.tmux.conf" ]; then mv -f "$HOME/.tmux.conf" "$HOME/.tmux.conf.ws-bak"; fi
mv "$n" "$d"
PATH="$HOME/.local/bin:$PATH"
if command -v tmux >/dev/null 2>&1 && tmux has-session 2>/dev/null; then tmux source-file "$d/tmux.conf" || true; fi`
}

// WSConfigPaths are the parts of ~/.config/ws setup copies. Projects
// stay behind: they point at directories on this machine.
var WSConfigPaths = []string{"config.yaml", "layouts"}

// SyncWSConfigScript replaces config.yaml and layouts/ in ~/.config/ws
// with the tar on stdin, leaving the host's projects alone.
func SyncWSConfigScript(hash string) string {
	return `set -e
d="$HOME/.config/ws"; n="$d/.ws-new"
mkdir -p "$d"; rm -rf "$n"; mkdir -p "$n"
tar -xzf - -C "$n"
for f in ` + strings.Join(WSConfigPaths, " ") + `; do
  if [ -e "$d/$f" ] && [ ! -f "$d/` + StampFile + `" ]; then rm -rf "$d/$f.ws-bak"; mv "$d/$f" "$d/$f.ws-bak"; else rm -rf "$d/$f"; fi
  if [ -e "$n/$f" ]; then mv "$n/$f" "$d/$f"; fi
done
printf %s ` + tmux.Quote(hash) + ` > "$d/` + StampFile + `"
rm -rf "$n"`
}

// TerminfoSource is the source of the terminfo entries named, as this
// machine has them.
func TerminfoSource(terms []string) ([]byte, error) {
	var buf bytes.Buffer
	for _, t := range terms {
		out, err := exec.Command("infocmp", "-x", t).Output()
		if err != nil {
			return nil, fmt.Errorf("infocmp %s: %w", t, err)
		}
		buf.Write(out)
	}
	return buf.Bytes(), nil
}

// HaveTerminfo filters terms to those this machine has an entry for.
func HaveTerminfo(terms []string) []string {
	var out []string
	for _, t := range terms {
		if t != "" && exec.Command("infocmp", t).Run() == nil {
			out = append(out, t)
		}
	}
	return out
}
