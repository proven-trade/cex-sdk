package kraken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// ExportReport는 내보낼 데이터 종류다.
type ExportReport string

const (
	ExportReportTrades  ExportReport = "trades"
	ExportReportLedgers ExportReport = "ledgers"
)

// ExportFormat은 내보내기 파일 형식이다.
type ExportFormat string

const (
	ExportFormatCSV ExportFormat = "CSV"
	ExportFormatTSV ExportFormat = "TSV"
)

// ExportRemoveAction은 보고서 삭제 방식이다.
type ExportRemoveAction string

const (
	// ExportRemoveCancel은 대기 중이거나 생성 중인 보고서를 취소한다.
	ExportRemoveCancel ExportRemoveAction = "cancel"
	// ExportRemoveDelete는 생성이 끝난 보고서를 삭제한다.
	ExportRemoveDelete ExportRemoveAction = "delete"
)

var (
	exportTradeFields  = []string{"ordertxid", "time", "ordertype", "price", "cost", "fee", "vol", "margin", "misc", "ledgers"}
	exportLedgerFields = []string{"refid", "time", "type", "subtype", "aclass", "asset", "amount", "fee", "balance", "wallet"}
)

// AddExportRequest는 거래 또는 원장 보고서 생성 요청이다.
type AddExportRequest struct {
	Report      ExportReport
	Description string
	// Format을 생략하면 CSV다.
	Format ExportFormat
	// Fields를 생략하면 모든 필드를 포함한다.
	Fields []string
	// Start를 생략하면 이번 달 1일, End를 생략하면 현재 시각이다.
	Start *time.Time
	End   *time.Time
}

// ExportReference는 생성을 요청한 보고서 ID다.
type ExportReference struct {
	ID  string          `json:"id"`
	Raw json.RawMessage `json:"-"`
}

// ExportStatusRequest는 보고서 생성 상태 조회 조건이다.
type ExportStatusRequest struct {
	Report ExportReport
}

// ExportStatus는 보고서 한 건의 생성 상태다. 시각은 Unix 초 문자열이다.
// Status는 Queued, Processing, Processed 중 하나이며 실패한 보고서는 Error에 사유가 담긴다.
type ExportStatus struct {
	ID           string          `json:"id"`
	Description  string          `json:"descr"`
	Format       string          `json:"format"`
	Report       string          `json:"report"`
	Subtype      string          `json:"subtype"`
	Status       string          `json:"status"`
	Error        string          `json:"error"`
	Fields       string          `json:"fields"`
	CreatedTime  string          `json:"createdtm"`
	StartTime    string          `json:"starttm"`
	CompletedAt  string          `json:"completedtm"`
	DataStart    string          `json:"datastarttm"`
	DataEnd      string          `json:"dataendtm"`
	EndTime      string          `json:"endtm"`
	Asset        string          `json:"asset"`
	AssetClasses []string        `json:"asset_classes"`
	Delete       bool            `json:"delete"`
	Raw          json.RawMessage `json:"-"`
}

// RetrieveExportRequest는 생성이 끝난 보고서 다운로드 조건이다.
type RetrieveExportRequest struct {
	ID string
}

// ExportFile은 다운로드한 보고서다. Data는 보고서가 담긴 zip 아카이브 원문이다.
type ExportFile struct {
	ContentType string
	Data        []byte
}

// RemoveExportRequest는 보고서 취소 또는 삭제 요청이다.
type RemoveExportRequest struct {
	ID     string
	Action ExportRemoveAction
}

// RemoveExportResult는 보고서 취소 또는 삭제 결과다.
type RemoveExportResult struct {
	Deleted  bool            `json:"delete"`
	Canceled bool            `json:"cancel"`
	Raw      json.RawMessage `json:"-"`
}

func (report ExportReport) valid() bool {
	return report == ExportReportTrades || report == ExportReportLedgers
}

func (request AddExportRequest) validate() error {
	if !request.Report.valid() {
		return validationError("export report must be trades or ledgers")
	}
	if err := validateRequiredText("export description", request.Description); err != nil {
		return err
	}
	if request.Format != "" && request.Format != ExportFormatCSV && request.Format != ExportFormatTSV {
		return validationError("unsupported export format %q", request.Format)
	}
	allowed := exportTradeFields
	if request.Report == ExportReportLedgers {
		allowed = exportLedgerFields
	}
	seen := make(map[string]struct{}, len(request.Fields))
	for _, field := range request.Fields {
		if !slices.Contains(allowed, field) {
			return validationError("unsupported %s export field %q", request.Report, field)
		}
		if _, exists := seen[field]; exists {
			return validationError("duplicate export field %q", field)
		}
		seen[field] = struct{}{}
	}
	return validateTimeRange(request.Start, request.End)
}

func (request AddExportRequest) values() url.Values {
	values := url.Values{
		"report":      {string(request.Report)},
		"description": {request.Description},
	}
	setIfNotEmpty(values, "format", string(request.Format))
	if len(request.Fields) > 0 {
		values.Set("fields", strings.Join(request.Fields, ","))
	}
	setTime(values, "starttm", request.Start)
	setTime(values, "endtm", request.End)
	return values
}

func (request ExportStatusRequest) validate() error {
	if !request.Report.valid() {
		return validationError("export report must be trades or ledgers")
	}
	return nil
}

func (request ExportStatusRequest) values() url.Values {
	return url.Values{"report": {string(request.Report)}}
}

func (request RetrieveExportRequest) validate() error {
	if !transactionIDPattern.MatchString(request.ID) {
		return validationError("export ID is required and contains unsupported characters")
	}
	return nil
}

func (request RetrieveExportRequest) values() url.Values {
	return url.Values{"id": {request.ID}}
}

func (request RemoveExportRequest) validate() error {
	if !transactionIDPattern.MatchString(request.ID) {
		return validationError("export ID is required and contains unsupported characters")
	}
	if request.Action != ExportRemoveCancel && request.Action != ExportRemoveDelete {
		return validationError("export remove action must be cancel or delete")
	}
	return nil
}

func (request RemoveExportRequest) values() url.Values {
	return url.Values{"id": {request.ID}, "type": {string(request.Action)}}
}

// AddExport는 거래 또는 원장 보고서 생성을 요청한다. 보고서가 만들어졌는지는 ExportStatus로 확인한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AddExport(
	ctx context.Context,
	request AddExportRequest,
	options ...trade.RequestOption,
) (ExportReference, error) {
	if err := request.validate(); err != nil {
		return ExportReference{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"AddExport", request.values(), credential.PermissionRead,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return ExportReference{}, err
	}
	var reference ExportReference
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &reference); err != nil {
		return ExportReference{}, err
	}
	if reference.ID == "" {
		return ExportReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken export acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

// ExportStatus는 요청한 보고서의 생성 상태를 조회한다.
func (client *Client) ExportStatus(
	ctx context.Context,
	request ExportStatusRequest,
	options ...trade.RequestOption,
) ([]ExportStatus, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"ExportStatus", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &items); err != nil {
		return nil, err
	}
	statuses := make([]ExportStatus, len(items))
	for index, item := range items {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &statuses[index],
		); err != nil {
			return nil, err
		}
		statuses[index].Raw = cloneBytes(item)
	}
	return statuses, nil
}

// RetrieveExport는 생성이 끝난 보고서 zip 아카이브를 내려받는다.
// 성공 응답은 JSON envelope가 아닌 바이너리 본문이다.
func (client *Client) RetrieveExport(
	ctx context.Context,
	request RetrieveExportRequest,
	options ...trade.RequestOption,
) (ExportFile, error) {
	if err := request.validate(); err != nil {
		return ExportFile{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"RetrieveExport", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return ExportFile{}, err
	}
	successful := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	if !successful || startsWithJSONObject(response.Body) {
		// 오류 envelope이거나 예상하지 못한 JSON 본문이다. 오류 분류는 공통 decode 규칙을 따른다.
		if _, decodeErr := client.decodeResult(response, commonexchange.OperationRead); decodeErr != nil {
			return ExportFile{}, decodeErr
		}
		return ExportFile{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken export download returned JSON instead of an archive"),
		)
	}
	if len(response.Body) == 0 {
		return ExportFile{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken export download is empty"),
		)
	}
	return ExportFile{
		ContentType: response.Header.Get("Content-Type"),
		Data:        cloneBytes(response.Body),
	}, nil
}

// RemoveExport는 대기 중인 보고서를 취소하거나 생성이 끝난 보고서를 삭제한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) RemoveExport(
	ctx context.Context,
	request RemoveExportRequest,
	options ...trade.RequestOption,
) (RemoveExportResult, error) {
	if err := request.validate(); err != nil {
		return RemoveExportResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"RemoveExport", request.values(), credential.PermissionRead,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return RemoveExportResult{}, err
	}
	var removed RemoveExportResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &removed); err != nil {
		return RemoveExportResult{}, err
	}
	if !removed.Deleted && !removed.Canceled {
		return RemoveExportResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken export removal was not acknowledged"),
		)
	}
	removed.Raw = cloneBytes(result)
	return removed, nil
}

func startsWithJSONObject(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '{'
}
