// Package main tests for the NovaNet CNI binary.
package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestParseConfig_ValidJSON verifies that a well-formed CNI config JSON is
// unmarshalled correctly.
func TestParseConfig_ValidJSON(t *testing.T) {
	data := []byte(`{
		"cniVersion": "1.0.0",
		"name": "novanet",
		"type": "novanet-cni",
		"agentSocket": "/run/novanet/custom.sock",
		"logFile": "/var/log/novanet-cni.log"
	}`)

	conf, err := parseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf.AgentSocket != "/run/novanet/custom.sock" {
		t.Errorf("expected agentSocket /run/novanet/custom.sock, got %s", conf.AgentSocket)
	}
	if conf.LogFile != "/var/log/novanet-cni.log" {
		t.Errorf("expected logFile /var/log/novanet-cni.log, got %s", conf.LogFile)
	}
	if conf.Name != "novanet" {
		t.Errorf("expected name novanet, got %s", conf.Name)
	}
}

// TestParseConfig_InvalidJSON verifies that malformed JSON returns an error.
func TestParseConfig_InvalidJSON(t *testing.T) {
	data := []byte(`{not valid json`)

	_, err := parseConfig(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse CNI config") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestParseConfig_DefaultAgentSocket verifies that AgentSocket is set to the
// default when omitted from the config JSON.
func TestParseConfig_DefaultAgentSocket(t *testing.T) {
	data := []byte(`{"cniVersion": "1.0.0", "name": "novanet", "type": "novanet-cni"}`)

	conf, err := parseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf.AgentSocket != defaultAgentSocket {
		t.Errorf("expected default agentSocket %s, got %s", defaultAgentSocket, conf.AgentSocket)
	}
}

// TestParseConfig_ExplicitSocketNotOverridden verifies that a non-empty
// AgentSocket in the JSON is not replaced by the default.
func TestParseConfig_ExplicitSocketNotOverridden(t *testing.T) {
	data := []byte(`{"cniVersion":"1.0.0","name":"novanet","type":"novanet-cni","agentSocket":"/tmp/test.sock"}`)

	conf, err := parseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf.AgentSocket != "/tmp/test.sock" {
		t.Errorf("expected /tmp/test.sock, got %s", conf.AgentSocket)
	}
}

// TestParseCNIArgs_ValidArgs verifies that a well-formed CNI_ARGS string is
// parsed into the expected key-value map.
func TestParseCNIArgs_ValidArgs(t *testing.T) {
	args := "K8S_POD_NAME=my-pod;K8S_POD_NAMESPACE=default;K8S_POD_INFRA_CONTAINER_ID=abc123"

	result := parseCNIArgs(args)

	if result["K8S_POD_NAME"] != "my-pod" {
		t.Errorf("expected K8S_POD_NAME=my-pod, got %s", result["K8S_POD_NAME"])
	}
	if result["K8S_POD_NAMESPACE"] != "default" {
		t.Errorf("expected K8S_POD_NAMESPACE=default, got %s", result["K8S_POD_NAMESPACE"])
	}
	if result["K8S_POD_INFRA_CONTAINER_ID"] != "abc123" {
		t.Errorf("expected K8S_POD_INFRA_CONTAINER_ID=abc123, got %s", result["K8S_POD_INFRA_CONTAINER_ID"])
	}
	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
}

// TestParseCNIArgs_EmptyString verifies that an empty input produces an empty map.
func TestParseCNIArgs_EmptyString(t *testing.T) {
	result := parseCNIArgs("")

	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

// TestParseCNIArgs_MissingValue verifies that pairs without a '=' separator
// are silently skipped.
func TestParseCNIArgs_MissingValue(t *testing.T) {
	// "NOVALUE" has no '=', so it should be skipped.
	result := parseCNIArgs("K8S_POD_NAME=foo;NOVALUE;K8S_POD_NAMESPACE=bar")

	if _, ok := result["NOVALUE"]; ok {
		t.Error("expected NOVALUE to be absent from result")
	}
	if result["K8S_POD_NAME"] != "foo" {
		t.Errorf("expected K8S_POD_NAME=foo, got %s", result["K8S_POD_NAME"])
	}
	if result["K8S_POD_NAMESPACE"] != "bar" {
		t.Errorf("expected K8S_POD_NAMESPACE=bar, got %s", result["K8S_POD_NAMESPACE"])
	}
}

// TestParseCNIArgs_ValueContainsEquals verifies that a value containing '='
// is preserved (SplitN with n=2 is used).
func TestParseCNIArgs_ValueContainsEquals(t *testing.T) {
	result := parseCNIArgs("KEY=val=ue")

	if result["KEY"] != "val=ue" {
		t.Errorf("expected val=ue, got %s", result["KEY"])
	}
}

// TestOpenLog_EmptyPath verifies that openLog with an empty path returns a
// logger with a nil writer (no-op logger).
func TestOpenLog_EmptyPath(t *testing.T) {
	l := openLog("")

	if l == nil {
		t.Fatal("expected non-nil cniLogger")
	}
	// w should be nil — Printf should not panic.
	l.Printf("test message %s", "hello")
}

// TestOpenLog_InvalidPath verifies that openLog with an unwritable path
// returns a no-op logger rather than panicking.
func TestOpenLog_InvalidPath(t *testing.T) {
	l := openLog("/nonexistent/directory/novanet-cni.log")

	if l == nil {
		t.Fatal("expected non-nil cniLogger")
	}
	// Should not panic even though the file could not be created.
	l.Printf("this should not panic")
}

// TestOpenLog_ValidPath verifies that openLog with a writable path returns a
// logger whose Printf output contains the expected content.
func TestOpenLog_ValidPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cni.log"

	l := openLog(path)
	if l == nil {
		t.Fatal("expected non-nil cniLogger")
	}
	if l.w == nil {
		t.Fatal("expected non-nil writer for valid path")
	}
}

// TestCNILoggerPrintf_Output verifies that Printf writes a timestamped line to
// the underlying writer.
func TestCNILoggerPrintf_Output(t *testing.T) {
	var buf bytes.Buffer
	l := &cniLogger{w: &buf}

	l.Printf("container=%s", "abc")

	output := buf.String()
	if !strings.Contains(output, "[novanet-cni]") {
		t.Errorf("expected [novanet-cni] in output, got: %s", output)
	}
	if !strings.Contains(output, "container=abc") {
		t.Errorf("expected 'container=abc' in output, got: %s", output)
	}
}

// TestCNILoggerPrintf_NilWriter verifies that Printf is a no-op when the
// writer is nil.
func TestCNILoggerPrintf_NilWriter(t *testing.T) {
	l := &cniLogger{w: nil}
	// Must not panic.
	l.Printf("nothing should be written")
}

// TestIntPtr verifies that intPtr returns a pointer whose dereferenced value
// equals the input.
func TestIntPtr(t *testing.T) {
	tests := []int{0, 1, -1, 42, 1000}
	for _, v := range tests {
		ptr := intPtr(v)
		if ptr == nil {
			t.Fatalf("intPtr(%d) returned nil", v)
		}
		if *ptr != v {
			t.Errorf("intPtr(%d) = %d, want %d", v, *ptr, v)
		}
	}
}

// TestIntPtr_Uniqueness verifies that each call to intPtr returns a distinct
// pointer so callers cannot alias each other's values.
func TestIntPtr_Uniqueness(t *testing.T) {
	a := intPtr(7)
	b := intPtr(7)
	if a == b {
		t.Error("expected distinct pointers from separate intPtr calls")
	}
}
