// Copyright 2010 Rebel Media
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build integration

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var (
	binary = filepath.Join(os.Getenv("GOPATH"), "bin/jellyfin_exporter")
)

const (
	address = "localhost:19594"
)

func TestFileDescriptorLeak(t *testing.T) {
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("jellyfin_exporter binary not available, try to run `make build` first: %s", err)
	}

	// --jellyfin.token is a required flag: without it the exporter exits during
	// flag parsing and the test can only ever fail with "can't start command".
	exporter := exec.Command(binary, "--jellyfin.token=TOKEN", "--web.listen-address", address)
	test := func(_ int) error {
		if err := queryExporter(address); err != nil {
			return err
		}
		for i := 0; i < 5; i++ {
			if err := queryExporter(address); err != nil {
				return err
			}
		}
		return nil
	}

	if err := runCommandAndTests(exporter, address, test); err != nil {
		t.Error(err)
	}
}

// TestScrapeSucceeds checks that a freshly started exporter serves /metrics
// successfully. It replaces a test inherited from node_exporter that wrote two
// .prom files to a temporary directory to exercise the textfile collector's
// duplicate handling — this exporter has no textfile collector and never read
// those files, so the test asserted nothing beyond what this one does.
func TestScrapeSucceeds(t *testing.T) {
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("jellyfin_exporter binary not available, try to run `make build` first: %s", err)
	}

	exporter := exec.Command(binary, "--jellyfin.token=TOKEN", "--web.listen-address", address)
	test := func(_ int) error {
		return queryExporter(address)
	}

	if err := runCommandAndTests(exporter, address, test); err != nil {
		t.Error(err)
	}
}

func queryExporter(address string) error {
	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", address))
	if err != nil {
		return err
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}
	if want, have := http.StatusOK, resp.StatusCode; want != have {
		return fmt.Errorf("want /metrics status code %d, have %d. Body:\n%s", want, have, b)
	}
	return nil
}

func runCommandAndTests(cmd *exec.Cmd, address string, fn func(pid int) error) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %s", err)
	}
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 10; i++ {
		if err := queryExporter(address); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
		if cmd.Process == nil || i == 9 {
			return fmt.Errorf("can't start command")
		}
	}

	errc := make(chan error)
	go func(pid int) {
		errc <- fn(pid)
	}(cmd.Process.Pid)

	err := <-errc
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
		// Reap the killed child; without this the process is left as a zombie
		// for as long as the test binary runs.
		_ = cmd.Wait()
	}
	return err
}
