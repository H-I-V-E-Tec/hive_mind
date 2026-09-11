package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"qdrant-mcp-server/internal/securitygate"
)

func main() {
	version := flag.String("version", "", "release version")
	digestOnly := flag.Bool("digest", false, "print tracked source digest for operational evidence")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	paths, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		fail("cannot enumerate release source")
	}
	digest, err := securitygate.Digest(".", strings.Split(strings.TrimRight(string(paths), "\x00"), "\x00"))
	if err != nil {
		fail("cannot hash release source")
	}
	if *digestOnly {
		fmt.Println(digest)
		return
	}
	f, err := os.Open("docs/operations/release-evidence.json")
	if err != nil {
		fail("release blocked: operational evidence is missing")
	}
	defer f.Close()
	report, err := securitygate.Read(f)
	if err != nil {
		fail(err.Error())
	}
	failures := securitygate.Validate(".", report, *version, digest, time.Now())
	if len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}
	fmt.Println("security evidence gates passed")
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
