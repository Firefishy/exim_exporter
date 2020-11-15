package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/promlog"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isCaseSensitiveFilesystem(inputPath string) (bool, error) {
	fh, err := os.Create(filepath.Join(inputPath, "test"))
	if err != nil {
		return false, err
	}
	fh.Close()
	_, err = os.Stat(filepath.Join(inputPath, "TEST"))
	if err == nil {
		return false, nil
	} else if os.IsNotExist(err) {
		return true, nil
	} else {
		return false, err
	}
}

func writeMockMessage(path string, hash string, index int) error {
	msgName := ""
	for i := 0; i < 4; i++ {
		msgName += string(BASE62[rand.Intn(62)])
	}
	// Add one deterministic char to prevent collisions
	msgName += string(BASE62[index])
	// Add the last char of the first segment should match our hash dir
	msgName += hash + "-"
	for i := 0; i < 6; i++ {
		msgName += string(BASE62[rand.Intn(62)])
	}
	msgName += "-"
	for i := 0; i < 2; i++ {
		msgName += string(BASE62[rand.Intn(62)])
	}
	for _, fileType := range "HD" {
		fileName := msgName + "-" + string(fileType)
		fh, err := os.Create(filepath.Join(path, fileName))
		if err != nil {
			return err
		}
		fh.Close()
	}
	return nil
}
func buildMockInput(inputPath string) error {
	// Write out test messages to a standard hash dir structure
	for h := 0; h < 62; h++ {
		hashChar := string(BASE62[h])
		hashPath := filepath.Join(inputPath, hashChar)
		if err := os.MkdirAll(hashPath, 0755); err != nil {
			return err
		}
		for i := 0; i <= h%3; i++ {
			if err := writeMockMessage(hashPath, hashChar, i); err != nil {
				return err
			}
		}
	}
	// Write out a couple messages using the single dir pattern
	for i := 0; i < 3; i++ {
		hashChar := string(BASE62[rand.Intn(62)])
		if err := writeMockMessage(inputPath, hashChar, i); err != nil {
			return err
		}
	}
	return nil
}

func collectAndCompareTestCase(name string, gatherer prometheus.Gatherer, t *testing.T) {
	metrics, err := os.Open(filepath.Join("test", name+".metrics"))
	if err != nil {
		t.Fatalf("Error opening test metrics")
	}
	if err := testutil.GatherAndCompare(gatherer, metrics); err != nil {
		t.Fatal("Unexpected metrics returned:", err)
	}
}

func appendLog(name string, file *os.File, t *testing.T) {
	data, err := ioutil.ReadFile(filepath.Join("test", name))
	if err != nil {
		t.Fatal("Unable to read mainlog test data")
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal("Unable to read mainlog test data")
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestMetrics(t *testing.T) {
	logger := promlog.New(&promlog.Config{})

	// Create a temp dir for our mock data
	tempPath, err := ioutil.TempDir("", "exim_exporter_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempPath)
	inputPath := filepath.Join(tempPath, "input")
	if err := os.MkdirAll(inputPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Setup temporary log files so we can stream data into them
	mainlog, err := os.OpenFile(filepath.Join(tempPath, "mainlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()
	rejectlog, err := os.OpenFile(filepath.Join(tempPath, "rejectlog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()
	paniclog, err := os.OpenFile(filepath.Join(tempPath, "paniclog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer mainlog.Close()

	registry := prometheus.NewPedanticRegistry()
	exporter := NewExporter(
		mainlog.Name(),
		rejectlog.Name(),
		paniclog.Name(),
		"exim4",
		inputPath,
		logger,
	)
	exporter.Start()
	if err := registry.Register(exporter); err != nil {
		t.Fatal(err)
	}

	for _, metric := range []prometheus.Collector{eximMessages, eximReject, eximPanic} {
		if err := registry.Register(metric); err != nil {
			t.Fatal(err)
		}
	}

	getProcesses = func() ([]*Process, error) {
		return []*Process{
			{[]string{"/bin/bash", "-x"}, 1},
		}, nil
	}
	t.Run("down", func(t *testing.T) {
		collectAndCompareTestCase("down", registry, t)
	})
	caseSensitive, err := isCaseSensitiveFilesystem(inputPath)
	if err != nil {
		t.Fatal("Unable to detect case-insensitive filesystem", err)
	}
	if !caseSensitive {
		t.Fatal("Running tests on a case-insensitive filesystem is not supported.")
	}
	if err = buildMockInput(inputPath); err != nil {
		t.Fatal("Unable to create mock input:", err)
	}
	getProcesses = func() ([]*Process, error) {
		return []*Process{
			{[]string{"/bin/bash", "-x"}, 7},
			{[]string{"/usr/sbin/exim4"}, 2202},
			{[]string{"/usr/sbin/exim4", "-q30m"}, 2203},
			{[]string{"/usr/sbin/exim4", "-bd"}, 1},
			{[]string{"/usr/sbin/exim4", "-qG"}, 2211},
			{[]string{"/usr/sbin/exim4", "-Mc", "1jofsL-0006tb-8D"}, 2309},
			{[]string{"/usr/sbin/exim4", "-Mc", "1jofsL-0006tb-8D"}, 2315},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3147},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3148},
			{[]string{"/usr/sbin/exim4", "-bd"}, 3149},
		}, nil
	}
	t.Run("up", func(t *testing.T) {
		collectAndCompareTestCase("up", registry, t)
	})
	t.Run("tail", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		// TODO: Verify stats have been collected before polling.
		// There is currently a race condition waiting for inotify to trigger stats collection.
		time.Sleep(1 * time.Second)
		collectAndCompareTestCase("tail", registry, t)
	})
	t.Run("update", func(t *testing.T) {
		fmt.Println("---")
		appendLog("mainlog", mainlog, t)
		appendLog("rejectlog", rejectlog, t)
		appendLog("paniclog", paniclog, t)
		time.Sleep(1 * time.Second)
		collectAndCompareTestCase("update", registry, t)
	})
}
