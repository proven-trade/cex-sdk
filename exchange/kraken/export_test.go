package kraken

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestAddExportSendsFormAndDecodesReportID(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddExport": `{"error":[],"result":{"id":"TCJA"}}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionRead)
	start := time.Unix(1695728276, 0)
	end := time.Unix(1695828276, 0)
	reference, err := client.AddExport(context.Background(), AddExportRequest{
		Report: ExportReportTrades, Description: "yearly report", Format: ExportFormatTSV,
		Fields: []string{"ordertxid", "time", "price"}, Start: &start, End: &end,
	})
	if err != nil {
		t.Fatalf("AddExport() error = %v", err)
	}
	if reference.ID != "TCJA" || len(reference.Raw) == 0 {
		t.Fatalf("AddExport() = %+v", reference)
	}
	request := server.last(t)
	form := request.form(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"AddExport" ||
		form.Get("report") != "trades" || form.Get("description") != "yearly report" ||
		form.Get("format") != "TSV" || form.Get("fields") != "ordertxid,time,price" ||
		form.Get("starttm") != "1695728276" || form.Get("endtm") != "1695828276" || form.Get("nonce") == "" {
		t.Fatalf("AddExport request = %+v form = %v", request, form)
	}
}

func TestAddExportRejectsEmptyAcknowledgement(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddExport": `{"error":[],"result":{}}`,
	})
	client, _ := newExtClient(t, server.URL)
	_, err := client.AddExport(context.Background(), AddExportRequest{
		Report: ExportReportLedgers, Description: "ledger report",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("AddExport() error = %v, want unknown execution state", err)
	}
}

func TestExportStatusDecodesReports(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "ExportStatus": `{"error":[],"result":[{"id":"VSKC","descr":"my_trades_1","format":"CSV","report":"trades","subtype":"all","status":"Processed","flags":"0","fields":"all","createdtm":"1688669085","expiretm":"1688878685","starttm":"1688669093","completedtm":"1688669099","datastarttm":"1683556800","dataendtm":"1688669085","aclass":"forex","asset":"all","asset_classes":["currency"],"delete":false},{"id":"TCJA","descr":"queued","format":"TSV","report":"trades","status":"Queued","error":"EExport:Canceled"}]}`,
	})
	client, _ := newExtClient(t, server.URL)
	statuses, err := client.ExportStatus(context.Background(), ExportStatusRequest{Report: ExportReportTrades})
	if err != nil {
		t.Fatalf("ExportStatus() error = %v", err)
	}
	if len(statuses) != 2 || statuses[0].ID != "VSKC" || statuses[0].Description != "my_trades_1" ||
		statuses[0].Status != "Processed" || statuses[0].CompletedAt != "1688669099" ||
		statuses[0].DataStart != "1683556800" || statuses[0].DataEnd != "1688669085" ||
		len(statuses[0].AssetClasses) != 1 || statuses[1].Error != "EExport:Canceled" ||
		statuses[1].Status != "Queued" || len(statuses[0].Raw) == 0 {
		t.Fatalf("ExportStatus() = %+v", statuses)
	}
	if form := server.last(t).form(t); form.Get("report") != "trades" {
		t.Fatalf("ExportStatus form = %v", form)
	}
}

func TestRetrieveExportReturnsBinaryArchive(t *testing.T) {
	t.Parallel()

	archive := []byte("PK\x03\x04\x00binary-zip-content\xff")
	binaryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != privatePrefix+"RetrieveExport" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), "id=VSKC") {
			http.Error(writer, `{"error":["EGeneral:Invalid arguments"]}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/zip")
		_, _ = writer.Write(archive)
	}))
	t.Cleanup(binaryServer.Close)
	client, _ := newExtClient(t, binaryServer.URL)
	file, err := client.RetrieveExport(context.Background(), RetrieveExportRequest{ID: "VSKC"})
	if err != nil {
		t.Fatalf("RetrieveExport() error = %v", err)
	}
	if !bytes.Equal(file.Data, archive) || file.ContentType != "application/zip" {
		t.Fatalf("RetrieveExport() = %+v", file)
	}
}

func TestRetrieveExportClassifiesJSONErrorEnvelope(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "RetrieveExport": `{"error":["EGeneral:Permission denied"]}`,
	})
	client, _ := newExtClient(t, server.URL)
	_, err := client.RetrieveExport(context.Background(), RetrieveExportRequest{ID: "VSKC"})
	var apiErr *trade.APIError
	if !errors.As(err, &apiErr) || apiErr.ExchangeCode != "EGeneral:Permission denied" {
		t.Fatalf("RetrieveExport() error = %v, want exchange API error", err)
	}

	emptyServer := newExtServer(t, map[string]string{
		privatePrefix + "RetrieveExport": ``,
	})
	emptyClient, _ := newExtClient(t, emptyServer.URL)
	if _, err := emptyClient.RetrieveExport(context.Background(), RetrieveExportRequest{ID: "VSKC"}); err == nil {
		t.Fatal("RetrieveExport(empty body) error = nil, want error")
	}
}

func TestRemoveExportCancelsOrDeletes(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "RemoveExport": `{"error":[],"result":{"delete":true}}`,
	})
	client, _ := newExtClient(t, server.URL)
	removed, err := client.RemoveExport(context.Background(), RemoveExportRequest{
		ID: "VSKC", Action: ExportRemoveDelete,
	})
	if err != nil {
		t.Fatalf("RemoveExport() error = %v", err)
	}
	if !removed.Deleted || removed.Canceled || len(removed.Raw) == 0 {
		t.Fatalf("RemoveExport() = %+v", removed)
	}
	form := server.last(t).form(t)
	if form.Get("id") != "VSKC" || form.Get("type") != "delete" || form.Get("nonce") == "" {
		t.Fatalf("RemoveExport form = %v", form)
	}

	emptyServer := newExtServer(t, map[string]string{
		privatePrefix + "RemoveExport": `{"error":[],"result":{"cancel":false}}`,
	})
	emptyClient, _ := newExtClient(t, emptyServer.URL)
	_, err = emptyClient.RemoveExport(context.Background(), RemoveExportRequest{
		ID: "VSKC", Action: ExportRemoveCancel,
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("RemoveExport(unacknowledged) error = %v, want unknown execution state", err)
	}
}

func TestExportRequestValidation(t *testing.T) {
	t.Parallel()

	start := time.Unix(200, 0)
	end := time.Unix(100, 0)
	invalid := []error{
		(AddExportRequest{Description: "d"}).validate(),
		(AddExportRequest{Report: ExportReportTrades}).validate(),
		(AddExportRequest{Report: ExportReportTrades, Description: "d", Format: "XML"}).validate(),
		(AddExportRequest{Report: ExportReportTrades, Description: "d", Fields: []string{"asset"}}).validate(),
		(AddExportRequest{Report: ExportReportLedgers, Description: "d", Fields: []string{"ordertxid"}}).validate(),
		(AddExportRequest{Report: ExportReportTrades, Description: "d", Fields: []string{"time", "time"}}).validate(),
		(AddExportRequest{Report: ExportReportTrades, Description: "d", Start: &start, End: &end}).validate(),
		(ExportStatusRequest{}).validate(),
		(RetrieveExportRequest{ID: "bad id"}).validate(),
		(RemoveExportRequest{ID: "VSKC"}).validate(),
		(RemoveExportRequest{ID: "VSKC", Action: "purge"}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	valid := []error{
		(AddExportRequest{Report: ExportReportLedgers, Description: "d", Fields: []string{"refid", "wallet"}}).validate(),
		(AddExportRequest{Report: ExportReportTrades, Description: "d", Format: ExportFormatCSV}).validate(),
		(ExportStatusRequest{Report: ExportReportLedgers}).validate(),
		(RemoveExportRequest{ID: "VSKC", Action: ExportRemoveCancel}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
}
