package integration

import (
	"bufio"
	"encoding/json"
	"io/ioutil"
	"os"
	"time"

	. "github.com/onsi/gomega"
)

var (
	tempLogfile *os.File
)

func setupTempLogfile() error {
	file, err := ioutil.TempFile("", "router_error_log")
	if err != nil {
		return err
	}
	tempLogfile = file
	return nil
}

func resetTempLogfile() {
	tempLogfile.Seek(0,0)
	tempLogfile.Truncate(0)
}

func cleanupTempLogfile() {
	if tempLogfile != nil {
		tempLogfile.Close()
		os.Remove(tempLogfile.Name())
	}
}

type routerLogEntry struct {
	Timestamp time.Time              `json:"@timestamp"`
	Fields    map[string]interface{} `json:"@fields"`
}

func lastRouterErrorLogLine() ([]byte, error) {
	scanner := bufio.NewScanner(tempLogfile)
	var line []byte
	for scanner.Scan() {
		line = scanner.Bytes()
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return line, nil
}

func lastRouterErrorLogEntry() *routerLogEntry {
	line, _ := lastRouterErrorLogLine()
	if line == nil {
		return nil
	}
	var entry *routerLogEntry
	err := json.Unmarshal(line, &entry)
	Expect(err).To(BeNil())
	return entry
}
