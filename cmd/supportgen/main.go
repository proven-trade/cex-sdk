package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	exchangeIDPattern = regexp.MustCompile(`^[a-z0-9]+$`)
	productIDPattern  = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
)

type catalog struct {
	Version  int       `json:"version"`
	Products []product `json:"products"`
}

type product struct {
	ExchangeID       string   `json:"exchange_id"`
	DisplayName      string   `json:"display_name"`
	Tier             string   `json:"tier"`
	ProductID        string   `json:"product_id"`
	ProductName      string   `json:"product_name"`
	REST             string   `json:"rest"`
	WebSocketPublic  string   `json:"websocket_public"`
	WebSocketPrivate string   `json:"websocket_private"`
	Unified          string   `json:"unified"`
	AutomatedTests   string   `json:"automated_tests"`
	LiveReadSmoke    string   `json:"live_read_smoke"`
	LiveTradeSmoke   string   `json:"live_trade_smoke"`
	Docs             []string `json:"docs"`
}

func main() {
	configPath := flag.String("config", "config/exchange-support.yaml", "지원 설정 파일")
	goOutput := flag.String("go-out", "support/catalog_generated.go", "생성할 Go 파일")
	markdownOutput := flag.String("md-out", "docs/SUPPORT_MATRIX.md", "생성할 Markdown 파일")
	check := flag.Bool("check", false, "생성 결과와 저장된 파일의 일치 여부만 검사")
	flag.Parse()
	if err := run(*configPath, *goOutput, *markdownOutput, *check); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(configPath, goOutput, markdownOutput string, check bool) error {
	value, err := readCatalog(configPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(configPath))
	if filepath.Base(filepath.Dir(configPath)) != "config" {
		root = "."
	}
	if err := validateCatalog(value, root); err != nil {
		return err
	}
	goData, err := renderGo(value)
	if err != nil {
		return err
	}
	markdownData := renderMarkdown(value)
	if check {
		if err := compareFile(goOutput, goData); err != nil {
			return err
		}
		return compareFile(markdownOutput, markdownData)
	}
	if err := writeFile(goOutput, goData); err != nil {
		return err
	}
	return writeFile(markdownOutput, markdownData)
}

func readCatalog(path string) (catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return catalog{}, fmt.Errorf("지원 설정 열기: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var value catalog
	if err := decoder.Decode(&value); err != nil {
		return catalog{}, fmt.Errorf("지원 설정 해석: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return catalog{}, err
	}
	return value, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("지원 설정 뒤에 추가 JSON 값이 있습니다")
		}
		return fmt.Errorf("지원 설정 끝 확인: %w", err)
	}
	return nil
}

func validateCatalog(value catalog, root string) error {
	if value.Version != 1 {
		return fmt.Errorf("지원 설정 version은 1이어야 합니다")
	}
	if len(value.Products) == 0 {
		return errors.New("지원 설정 products가 비어 있습니다")
	}
	seen := make(map[string]struct{}, len(value.Products))
	lastTier := 0
	for index, item := range value.Products {
		if !exchangeIDPattern.MatchString(item.ExchangeID) {
			return fmt.Errorf("products[%d] exchange_id가 올바르지 않습니다", index)
		}
		if strings.TrimSpace(item.DisplayName) == "" || strings.TrimSpace(item.ProductName) == "" {
			return fmt.Errorf("products[%d] 표시 이름이 비어 있습니다", index)
		}
		tier, err := tierNumber(item.Tier)
		if err != nil {
			return fmt.Errorf("products[%d]: %w", index, err)
		}
		if tier < lastTier {
			return fmt.Errorf("products[%d] tier 순서가 이전 항목보다 낮습니다", index)
		}
		lastTier = tier
		if !productIDPattern.MatchString(item.ProductID) {
			return fmt.Errorf("products[%d] product_id가 올바르지 않습니다", index)
		}
		key := item.ExchangeID + "/" + item.ProductID
		if _, exists := seen[key]; exists {
			return fmt.Errorf("중복된 거래소 상품 %s", key)
		}
		seen[key] = struct{}{}
		statuses := []string{
			item.REST, item.WebSocketPublic, item.WebSocketPrivate, item.Unified,
			item.AutomatedTests, item.LiveReadSmoke, item.LiveTradeSmoke,
		}
		for _, status := range statuses {
			if !validStatus(status) {
				return fmt.Errorf("products[%d]에 올바르지 않은 상태 %q가 있습니다", index, status)
			}
		}
		if item.REST == "implemented" && item.AutomatedTests != "implemented" {
			return fmt.Errorf("products[%d] REST 구현에는 자동 테스트 완료가 필요합니다", index)
		}
		for _, doc := range item.Docs {
			if filepath.IsAbs(doc) || filepath.Ext(doc) != ".md" || strings.Contains(doc, "..") {
				return fmt.Errorf("products[%d] 문서 경로 %q가 올바르지 않습니다", index, doc)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(doc))); err != nil {
				return fmt.Errorf("products[%d] 문서 경로 %q 확인: %w", index, doc, err)
			}
		}
	}
	return nil
}

func tierNumber(tier string) (int, error) {
	if len(tier) != 2 || tier[0] != 'P' || tier[1] < '0' || tier[1] > '9' {
		return 0, fmt.Errorf("올바르지 않은 tier %q", tier)
	}
	return int(tier[1] - '0'), nil
}

func validStatus(status string) bool {
	return status == "implemented" || status == "planned" ||
		status == "pending" || status == "not_applicable"
}

func renderGo(value catalog) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("// 이 파일은 config/exchange-support.yaml에서 자동 생성됩니다.\n")
	output.WriteString("package support\n\n")
	output.WriteString("import \"github.com/proven-trade/proven-trade-sdk/model\"\n\n")
	output.WriteString("var catalogData = []ProductSupport{\n")
	for _, item := range value.Products {
		output.WriteString("\t{\n")
		_, _ = fmt.Fprintf(&output, "\t\tExchange: model.ExchangeID(%s), DisplayName: %s, Tier: %s,\n",
			strconv.Quote(item.ExchangeID), strconv.Quote(item.DisplayName), strconv.Quote(item.Tier))
		_, _ = fmt.Fprintf(&output, "\t\tProduct: ProductID(%s), ProductName: %s,\n",
			strconv.Quote(item.ProductID), strconv.Quote(item.ProductName))
		_, _ = fmt.Fprintf(&output, "\t\tREST: Status(%s), WebSocketPublic: Status(%s), WebSocketPrivate: Status(%s),\n",
			strconv.Quote(item.REST), strconv.Quote(item.WebSocketPublic), strconv.Quote(item.WebSocketPrivate))
		_, _ = fmt.Fprintf(&output, "\t\tUnified: Status(%s), AutomatedTests: Status(%s),\n",
			strconv.Quote(item.Unified), strconv.Quote(item.AutomatedTests))
		_, _ = fmt.Fprintf(&output, "\t\tLiveReadSmoke: Status(%s), LiveTradeSmoke: Status(%s),\n",
			strconv.Quote(item.LiveReadSmoke), strconv.Quote(item.LiveTradeSmoke))
		output.WriteString("\t\tDocs: []string{")
		for index, doc := range item.Docs {
			if index > 0 {
				output.WriteString(", ")
			}
			output.WriteString(strconv.Quote(doc))
		}
		output.WriteString("},\n\t},\n")
	}
	output.WriteString("}\n")
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("생성한 Go 코드 정리: %w", err)
	}
	return formatted, nil
}

func renderMarkdown(value catalog) []byte {
	var output strings.Builder
	output.WriteString("# 거래소 지원 매트릭스\n\n")
	output.WriteString("이 문서는 `config/exchange-support.yaml`에서 자동 생성됩니다. 직접 수정하지 않습니다.\n\n")
	output.WriteString("`구현`은 코드·자동 테스트·문서가 저장소에 있다는 뜻이며 운영 검증 완료를 뜻하지 않습니다. `읽기 smoke`와 `거래 smoke`가 모두 `구현`이어야 실제 계정과 지정 EIP를 이용한 운영 검증까지 끝난 상태입니다.\n\n")
	output.WriteString("| 등급 | 거래소 | 상품 | REST | WS public | WS private | Unified | 자동 테스트 | 읽기 smoke | 거래 smoke | 문서 |\n")
	output.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, item := range value.Products {
		_, _ = fmt.Fprintf(&output, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			item.Tier, item.DisplayName, item.ProductName,
			statusLabel(item.REST), statusLabel(item.WebSocketPublic), statusLabel(item.WebSocketPrivate),
			statusLabel(item.Unified), statusLabel(item.AutomatedTests),
			statusLabel(item.LiveReadSmoke), statusLabel(item.LiveTradeSmoke), docsLabel(item.Docs))
	}
	implemented, planned := 0, 0
	for _, item := range value.Products {
		if item.REST == "implemented" {
			implemented++
		} else if item.REST == "planned" {
			planned++
		}
	}
	_, _ = fmt.Fprintf(&output, "\n현재 REST 구현 상품군은 %d개이고 계획 상품군은 %d개입니다.\n\n", implemented, planned)
	output.WriteString("상태 의미: `구현`은 저장소 구현 완료, `예정`은 계획됨, `대기`는 외부 환경이나 실제 계정 검증 대기, `해당 없음`은 공통 계약의 대상이 아님을 뜻합니다.\n")
	return []byte(output.String())
}

func statusLabel(status string) string {
	switch status {
	case "implemented":
		return "구현"
	case "planned":
		return "예정"
	case "pending":
		return "대기"
	default:
		return "해당 없음"
	}
}

func docsLabel(docs []string) string {
	if len(docs) == 0 {
		return "—"
	}
	values := make([]string, len(docs))
	for index, doc := range docs {
		values[index] = "[문서](" + filepath.ToSlash(strings.TrimPrefix(doc, "docs/")) + ")"
	}
	return strings.Join(values, "<br>")
}

func compareFile(path string, expected []byte) error {
	actual, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("생성 결과 확인 %s: %w", path, err)
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%s가 지원 설정과 다릅니다. supportgen을 실행하세요", path)
	}
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("출력 디렉터리 생성 %s: %w", path, err)
	}
	actual, err := os.ReadFile(path)
	if err == nil && bytes.Equal(actual, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("기존 출력 읽기 %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("출력 쓰기 %s: %w", path, err)
	}
	return nil
}
