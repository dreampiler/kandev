package api

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"

	"go.uber.org/goleak"
)

const apiTestVscodeFixtureEnv = "KANDEV_API_TEST_VSCODE_FIXTURE"

func TestMain(m *testing.M) {
	if os.Getenv(apiTestVscodeFixtureEnv) != "" {
		runAPITestVscodeFixture()
		return
	}
	goleak.VerifyTestMain(m)
}

func runAPITestVscodeFixture() {
	address := ""
	for i, arg := range os.Args[:len(os.Args)-1] {
		if arg == "--bind-addr" {
			address = os.Args[i+1]
			break
		}
	}
	if address == "" {
		fmt.Fprintln(os.Stderr, "VS Code fixture: missing --bind-addr")
		os.Exit(2)
	}
	// A wildcard test listener prompts for Windows Firewall access from each
	// temporary test binary; the fixture only needs a reachable bind address.
	if runtime.GOOS == "windows" {
		if host, port, err := net.SplitHostPort(address); err == nil && (host == "" || host == "0.0.0.0" || host == "::" || host == "[::]") {
			address = net.JoinHostPort("127.0.0.1", port)
		}
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VS Code fixture: listen: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}
