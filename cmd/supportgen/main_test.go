package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySupportOutputsAreCurrent(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("..", "..", "config", "exchange-support.yaml")
	value, err := readCatalog(configPath)
	if err != nil {
		t.Fatalf("readCatalog() error = %v", err)
	}
	root := filepath.Join("..", "..")
	if err := validateCatalog(value, root); err != nil {
		t.Fatalf("validateCatalog() error = %v", err)
	}
	goData, err := renderGo(value)
	if err != nil {
		t.Fatalf("renderGo() error = %v", err)
	}
	markdownData := renderMarkdown(value)
	assertGeneratedFile(
		t, filepath.Join(root, "support", "catalog_generated.go"), goData,
	)
	assertGeneratedFile(
		t, filepath.Join(root, "docs", "SUPPORT_MATRIX.md"), markdownData,
	)
}

func TestSupportCatalogRejectsUnknownAndInvalidValues(t *testing.T) {
	t.Parallel()

	temporary := t.TempDir()
	unknownPath := filepath.Join(temporary, "unknown.yaml")
	if err := os.WriteFile(unknownPath, []byte(`{"version":1,"unknown":true,"products":[]}`), 0o600); err != nil {
		t.Fatalf("write unknown config: %v", err)
	}
	if _, err := readCatalog(unknownPath); err == nil {
		t.Fatal("unknown field error = nil")
	}
	value := catalog{Version: 1, Products: []product{{
		ExchangeID: "Bad ID", DisplayName: "Bad", Tier: "P0",
		ProductID: "spot", ProductName: "Spot",
		REST: "implemented", WebSocketPublic: "planned", WebSocketPrivate: "planned",
		Unified: "planned", AutomatedTests: "planned",
		LiveReadSmoke: "pending", LiveTradeSmoke: "pending",
	}}}
	if err := validateCatalog(value, temporary); err == nil {
		t.Fatal("invalid exchange ID error = nil")
	}
	value.Products[0].ExchangeID = "valid"
	if err := validateCatalog(value, temporary); err == nil ||
		!strings.Contains(err.Error(), "자동 테스트") {
		t.Fatalf("implemented without tests error = %v", err)
	}
}

func TestSupportRenderingIsDeterministic(t *testing.T) {
	t.Parallel()

	value := catalog{Version: 1, Products: []product{{
		ExchangeID: "test", DisplayName: "Test", Tier: "P0",
		ProductID: "spot", ProductName: "Spot",
		REST: "planned", WebSocketPublic: "planned", WebSocketPrivate: "planned",
		Unified: "planned", AutomatedTests: "planned",
		LiveReadSmoke: "pending", LiveTradeSmoke: "pending",
	}}}
	firstGo, err := renderGo(value)
	if err != nil {
		t.Fatalf("first renderGo() error = %v", err)
	}
	secondGo, err := renderGo(value)
	if err != nil {
		t.Fatalf("second renderGo() error = %v", err)
	}
	if !bytes.Equal(firstGo, secondGo) {
		t.Fatal("Go rendering is not deterministic")
	}
	if !bytes.Equal(renderMarkdown(value), renderMarkdown(value)) {
		t.Fatal("Markdown rendering is not deterministic")
	}
}

func assertGeneratedFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file %s: %v", path, err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("generated file %s is stale", path)
	}
}
