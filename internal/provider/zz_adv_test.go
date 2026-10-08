package provider

import (
	"context"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/osshim"
)

type pr = osshim.Process

func TestAdv(t *testing.T) {
	cargo := []string{"cargo", "rustc"}
	type c struct {
		name  string
		procs []pr
		win   bool
		want  bool
	}
	self := func(cl string) pr { return pr{PID: 10, PPID: 9, CommandLine: cl, Args: cl} }
	anc := func(pid, ppid int, cl string) pr { return pr{PID: pid, PPID: ppid, CommandLine: cl, Args: cl} }
	cs := []c{
		{"quoted-ps", []pr{self(`bilgie --config /x y/c.yaml clean cargo`), anc(9, 1, `sh -c bilgie --config "/x y/c.yaml" clean cargo`)}, false, false},
		{"quoted-outer", []pr{self(`bilgie --config /x y/c.yaml clean cargo`), anc(9, 1, `sh -c 'bilgie --config "/x y/c.yaml" clean cargo'`)}, false, false},
		{"padded", []pr{self(`bilgie clean cargo`), anc(9, 1, `bash -c "bilgie  clean  cargo"`)}, false, false},
		{"padded-self", []pr{self(`bilgie  clean   cargo`), anc(9, 1, `bash -c bilgie  clean  cargo`)}, false, false},
		{"nested", []pr{self(`bilgie clean cargo`), anc(9, 8, `sh -c bilgie clean cargo`), anc(8, 1, `bash -c 'sh -c "bilgie clean cargo"'`)}, false, false},
		{"nested3", []pr{self(`bilgie clean cargo`), anc(9, 8, `sh -c 'sh -c "bilgie clean cargo"'`), anc(8, 7, `zsh -c "sh -c 'sh -c \"bilgie clean cargo\"'"`), anc(7, 1, "launchd")}, false, false},
		{"env", []pr{self(`bilgie clean cargo`), anc(9, 1, `env FOO=x bilgie clean cargo`)}, false, false},
		{"envsh", []pr{self(`bilgie clean cargo`), anc(9, 1, `env FOO=x sh -c 'bilgie clean cargo'`)}, false, false},
		{"sudo", []pr{self(`bilgie clean cargo`), anc(9, 1, `sudo bilgie clean cargo`)}, false, false},
		{"sudo-u", []pr{self(`bilgie clean cargo`), anc(9, 1, `sudo -u root bilgie clean cargo`)}, false, false},
		{"abs path", []pr{self(`/usr/local/bin/bilgie clean cargo`), anc(9, 1, `sh -c /usr/local/bin/bilgie clean cargo`)}, false, false},
		{"sibling cargo", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c bilgie clean cargo`), anc(20, 1, `cargo build`)}, false, true},
		{"cargo ancestor", []pr{self(`bilgie clean cargo`), anc(9, 8, `sh -c bilgie clean cargo`), anc(8, 1, `cargo run -- clean cargo`)}, false, true},
		{"cargo ancestor 2", []pr{self(`bilgie clean cargo`), anc(9, 8, `cargo`)}, false, true},
		{"wrapper also runs cargo unquoted", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c cargo build && bilgie clean cargo`)}, false, true},
		{"wrapper cargo then bilgie quoted", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c 'cargo build; bilgie clean cargo'`)}, false, true},
		{"unparseable", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c 'bilgie clean cargo`)}, false, true},
		{"apostrophe path", []pr{self(`bilgie --config /x/it's.yaml clean cargo`), anc(9, 1, `sh -c bilgie --config /x/it's.yaml clean cargo`)}, false, true},
		{"win", []pr{self(`C:\Program Files\bilgie\bilgie.exe clean cargo`), anc(9, 1, `cmd.exe /c C:\Program Files\bilgie\bilgie.exe clean cargo`)}, true, false},
		{"win quoted", []pr{self(`C:\bin\bilgie.exe --config C:\x y\c.yaml clean cargo`), anc(9, 1, `cmd /c "C:\bin\bilgie.exe" --config "C:\x y\c.yaml" clean cargo`)}, true, false},
		{"win upper", []pr{self(`C:\bin\BILGIE.EXE clean cargo`), anc(9, 1, `powershell -Command "bilgie clean cargo"`)}, true, false},
		{"win cargo.exe anc", []pr{self(`bilgie.exe clean cargo`), anc(9, 1, `C:\Users\a\.cargo\bin\cargo.exe run`)}, true, true},
		{"win cargo sibling", []pr{self(`bilgie.exe clean cargo`), anc(9, 1, `cmd /c bilgie.exe clean cargo`), anc(30, 1, `C:\x\RUSTC.EXE foo`)}, true, true},
		{"linux comm", []pr{{PID: 10, PPID: 9, CommandLine: `bilgie clean cargo bilgie`, Args: `bilgie clean cargo`}, {PID: 9, PPID: 1, CommandLine: `sh -c bilgie clean cargo sh`, Args: `sh -c bilgie clean cargo`}}, false, false},
		{"linux comm cargo anc", []pr{{PID: 10, PPID: 9, CommandLine: `bilgie clean cargo bilgie`, Args: `bilgie clean cargo`}, {PID: 9, PPID: 1, CommandLine: `cargo cargo`, Args: `cargo`}}, false, true},
		{"other bilgie anc", []pr{self(`bilgie clean cargo`), anc(9, 1, `bilgie auto cargo`)}, false, false},
		{"lookalike exe", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c notbilgie clean cargo`)}, false, true},
		{"args prefix", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c bilgie clean cargo-extra && cargo build`)}, false, true},
		{"self only", []pr{self(`bilgie clean cargo`)}, false, false},
		{"self missing", []pr{anc(9, 1, `sh -c bilgie clean cargo`)}, false, true},
		{"cargo in path", []pr{self(`bilgie --config /home/cargo/c.yaml clean cargo`), anc(9, 1, `sh -c bilgie --config /home/cargo/c.yaml clean cargo`)}, false, false},
		{"empty arg", []pr{self(`bilgie clean cargo`), anc(9, 1, `sh -c "bilgie clean cargo" ""`)}, false, false},
		{"tabs", []pr{self("bilgie\tclean cargo"), anc(9, 1, "sh -c 'bilgie\tclean\t\tcargo'")}, false, false},
		{"unicode", []pr{self(`bilgie --config /ü x/ç.yaml clean cargo`), anc(9, 1, `sh -c 'bilgie --config "/ü x/ç.yaml" clean cargo'`)}, false, false},
		{"cycle", []pr{{PID: 10, PPID: 9, CommandLine: `bilgie clean cargo`}, {PID: 9, PPID: 10, CommandLine: `sh -c bilgie clean cargo`}}, false, false},
	}
	for _, k := range cs {
		lines := excludeSelfFor(k.procs, 10, cargo, k.win)
		g := fakeGuard(lines, nil, nil)
		got := g.busyReason(context.Background()) != ""
		if got != k.want {
			t.Errorf("%s: busy=%v want %v lines=%q", k.name, got, k.want, lines)
		}
	}
}
