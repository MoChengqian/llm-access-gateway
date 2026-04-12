package redis

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadRESPReadsBulkString(t *testing.T) {
	value, err := readRESP(bufio.NewReader(strings.NewReader("$5\r\nhello\r\n")))
	if err != nil {
		t.Fatalf("read bulk string: %v", err)
	}

	got, ok := value.(string)
	if !ok || got != "hello" {
		t.Fatalf("expected bulk string hello, got %#v", value)
	}
}

func TestReadRESPReadsNestedArrays(t *testing.T) {
	value, err := readRESP(bufio.NewReader(strings.NewReader("*2\r\n:1\r\n*2\r\n+ok\r\n$5\r\nhello\r\n")))
	if err != nil {
		t.Fatalf("read nested array: %v", err)
	}

	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected two top-level items, got %#v", value)
	}

	if first, ok := items[0].(int64); !ok || first != 1 {
		t.Fatalf("expected first item 1, got %#v", items[0])
	}

	nested, ok := items[1].([]any)
	if !ok || len(nested) != 2 {
		t.Fatalf("expected nested array, got %#v", items[1])
	}

	if nested[0] != "ok" || nested[1] != "hello" {
		t.Fatalf("unexpected nested items %#v", nested)
	}
}

func TestReadRESPRejectsUnsupportedPrefix(t *testing.T) {
	_, err := readRESP(bufio.NewReader(strings.NewReader("?oops\r\n")))
	if err == nil || !strings.Contains(err.Error(), "unsupported redis response prefix") {
		t.Fatalf("expected unsupported prefix error, got %v", err)
	}
}
