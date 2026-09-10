package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const maxDiagnosticResponses = 200

type responseRecord struct {
	URL          string
	Method       string
	Status       int
	MimeType     string
	ResourceType string
}

type requestRecord struct {
	URL    string
	Method string
}

type diagnostics struct {
	mu sync.Mutex

	consoleErrors []ConsoleError
	networkErrors []NetworkError
	pageErrors    []PageError
	responses     []responseRecord
	requests      map[network.RequestID]requestRecord
	responseByID  map[network.RequestID]responseRecord
	responseWake  chan struct{}
}

func newDiagnostics() *diagnostics {
	return &diagnostics{
		requests:     make(map[network.RequestID]requestRecord),
		responseByID: make(map[network.RequestID]responseRecord),
		responseWake: make(chan struct{}, 1),
	}
}

func (d *diagnostics) attach(ctx context.Context) {
	chromedp.ListenTarget(ctx, d.recordEvent)
}

func (d *diagnostics) recordEvent(ev any) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch event := ev.(type) {
	case *network.EventRequestWillBeSent:
		if event.Request != nil {
			d.requests[event.RequestID] = requestRecord{URL: event.Request.URL, Method: event.Request.Method}
		}
	case *network.EventResponseReceived:
		if event.Response == nil {
			return
		}
		record := responseRecord{URL: event.Response.URL, Status: int(event.Response.Status), MimeType: event.Response.MimeType, ResourceType: string(event.Type)}
		if request, ok := d.requests[event.RequestID]; ok {
			record.Method = request.Method
		}
		if len(d.responses) == maxDiagnosticResponses {
			copy(d.responses, d.responses[1:])
			d.responses[len(d.responses)-1] = record
		} else {
			d.responses = append(d.responses, record)
		}
		d.responseByID[event.RequestID] = record
		select {
		case d.responseWake <- struct{}{}:
		default:
		}
	case *network.EventLoadingFinished:
		delete(d.requests, event.RequestID)
		delete(d.responseByID, event.RequestID)
	case *network.EventLoadingFailed:
		response, hasResponse := d.responseByID[event.RequestID]
		request := d.requests[event.RequestID]
		delete(d.requests, event.RequestID)
		delete(d.responseByID, event.RequestID)
		// Chromium 在部分平台会给无响应体的成功响应补发 ERR_ABORTED。
		// 例如 fetch 收到 204 后，ResponseReceived 已经证明请求成功，此事件不能再被当成网络失败。
		if hasResponse && benignAbortAfterNoBodyResponse(event.ErrorText, response) {
			return
		}
		d.networkErrors = appendBoundedNetwork(d.networkErrors, NetworkError{URL: request.URL, Method: request.Method, ErrorText: event.ErrorText})
	case *cdpruntime.EventConsoleAPICalled:
		if strings.EqualFold(string(event.Type), "error") || strings.EqualFold(string(event.Type), "assert") {
			d.consoleErrors = appendBoundedConsole(d.consoleErrors, ConsoleError{Message: consoleMessage(event.Args)})
		}
	case *cdpruntime.EventExceptionThrown:
		message := "unhandled page exception"
		if event.ExceptionDetails != nil {
			message = strings.TrimSpace(event.ExceptionDetails.Text)
			if event.ExceptionDetails.Exception != nil && strings.TrimSpace(event.ExceptionDetails.Exception.Description) != "" {
				message = strings.TrimSpace(event.ExceptionDetails.Exception.Description)
			}
		}
		d.pageErrors = appendBoundedPage(d.pageErrors, PageError{Message: message})
	case *log.EventEntryAdded:
		if event.Entry != nil && strings.EqualFold(string(event.Entry.Level), "error") {
			d.consoleErrors = appendBoundedConsole(d.consoleErrors, ConsoleError{Message: strings.TrimSpace(event.Entry.Text)})
		}
	}
}

func (d *diagnostics) enable(parent, pageCtx context.Context) error {
	return runWithContext(parent, pageCtx,
		network.Enable(),
		cdpruntime.Enable(),
		log.Enable(),
		page.Enable(),
		page.SetLifecycleEventsEnabled(true),
	)
}

func (d *diagnostics) snapshot() ([]ConsoleError, []NetworkEvent, []NetworkError, []PageError) {
	d.mu.Lock()
	defer d.mu.Unlock()
	events := make([]NetworkEvent, 0, len(d.responses))
	for _, response := range d.responses {
		events = append(events, NetworkEvent{URL: response.URL, Method: response.Method, Status: response.Status, MimeType: response.MimeType, ResourceType: response.ResourceType})
	}
	return append([]ConsoleError(nil), d.consoleErrors...), events, append([]NetworkError(nil), d.networkErrors...), append([]PageError(nil), d.pageErrors...)
}

func (d *diagnostics) hasMatchingResponse(action WaitResponseAction) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, response := range d.responses {
		if responseMatches(response, action) {
			return true
		}
	}
	return false
}

func benignAbortAfterNoBodyResponse(errorText string, response responseRecord) bool {
	if !strings.EqualFold(strings.TrimSpace(errorText), "net::ERR_ABORTED") {
		return false
	}
	if strings.EqualFold(response.Method, "HEAD") {
		return true
	}
	return (response.Status >= 100 && response.Status < 200) || response.Status == 204 || response.Status == 205 || response.Status == 304
}

func consoleMessage(args []*cdpruntime.RemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == nil {
			continue
		}
		text := strings.TrimSpace(arg.Description)
		if text == "" && len(arg.Value) != 0 {
			text = strings.TrimSpace(string(arg.Value))
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "console error"
	}
	return strings.Join(parts, " ")
}

func appendBoundedConsole(values []ConsoleError, value ConsoleError) []ConsoleError {
	if strings.TrimSpace(value.Message) == "" {
		return values
	}
	if len(values) >= 50 {
		return values
	}
	return append(values, value)
}

func appendBoundedNetwork(values []NetworkError, value NetworkError) []NetworkError {
	if len(values) >= 50 {
		return values
	}
	return append(values, value)
}

func appendBoundedPage(values []PageError, value PageError) []PageError {
	if len(values) >= 50 {
		return values
	}
	return append(values, value)
}

func formatResponse(record responseRecord) string {
	return fmt.Sprintf("%s %d %s", record.Method, record.Status, record.URL)
}
