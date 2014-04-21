// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testData uint32

func checkSymbols(t *testing.T, nmoutput []byte) {
	switch runtime.GOOS {
	case "linux", "darwin", "solaris":
		t.Skip("skipping test; see http://golang.org/issue/7829")
	}
	var checkSymbolsFound, testDataFound bool
	scanner := bufio.NewScanner(bytes.NewBuffer(nmoutput))
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) < 3 {
			t.Error("nm must have at least 3 columns")
			continue
		}
		switch f[2] {
		case "cmd/nm.checkSymbols":
			checkSymbolsFound = true
			addr := "0x" + f[0]
			if addr != fmt.Sprintf("%p", checkSymbols) {
				t.Errorf("nm shows wrong address %v for checkSymbols (%p)", addr, checkSymbols)
			}
		case "cmd/nm.testData":
			testDataFound = true
			addr := "0x" + f[0]
			if addr != fmt.Sprintf("%p", &testData) {
				t.Errorf("nm shows wrong address %v for testData (%p)", addr, &testData)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("error while reading symbols: %v", err)
		return
	}
	if !checkSymbolsFound {
		t.Error("nm shows no checkSymbols symbol")
	}
	if !testDataFound {
		t.Error("nm shows no testData symbol")
	}
}

func TestNM(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", "testnm.exe", "cmd/nm").CombinedOutput()
	if err != nil {
		t.Fatalf("go build -o testnm.exe cmd/nm: %v\n%s", err, string(out))
	}
	defer os.Remove("testnm.exe")

	testfiles := []string{
		"elf/testdata/gcc-386-freebsd-exec",
		"elf/testdata/gcc-amd64-linux-exec",
		"macho/testdata/gcc-386-darwin-exec",
		"macho/testdata/gcc-amd64-darwin-exec",
		"pe/testdata/gcc-amd64-mingw-exec",
		"pe/testdata/gcc-386-mingw-exec",
		"plan9obj/testdata/amd64-plan9-exec",
		"plan9obj/testdata/386-plan9-exec",
	}
	for _, f := range testfiles {
		exepath := filepath.Join(runtime.GOROOT(), "src", "pkg", "debug", f)
		cmd := exec.Command("./testnm.exe", exepath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go tool nm %v: %v\n%s", exepath, err, string(out))
		}
	}

	cmd := exec.Command("./testnm.exe", os.Args[0])
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go tool nm %v: %v\n%s", os.Args[0], err, string(out))
	}
	checkSymbols(t, out)
}
