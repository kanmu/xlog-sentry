package xlogsentry

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/getsentry/sentry-go"
	"github.com/rs/xlog"
)

var (
	xlogSeverityMap = map[string]xlog.Level{
		"debug": xlog.LevelDebug,
		"info":  xlog.LevelInfo,
		"warn":  xlog.LevelWarn,
		"error": xlog.LevelError,
	}

	severityMap = map[xlog.Level]sentry.Level{
		xlog.LevelDebug: sentry.LevelDebug,
		xlog.LevelInfo:  sentry.LevelInfo,
		xlog.LevelWarn:  sentry.LevelWarning,
		xlog.LevelError: sentry.LevelError,
	}
)

// Output is a xlog to sentry output
type Output struct {
	Timeout                 time.Duration
	StacktraceConfiguration StackTraceConfiguration
	Level                   xlog.Level

	client *sentry.Client
	host   string
}

// StackTraceConfiguration allows for configuring stacktraces
type StackTraceConfiguration struct {
	// whether stacktraces should be enabled
	Enable bool
	// the level at which to start capturing stacktraces
	Level xlog.Level
	// how many stack frames to skip before stacktrace starts recording
	Skip int
	// the number of lines to include around a stack frame for context
	Context int
	// the prefixes that will be matched against the stack frame.
	// if the stack frame's package matches one of these prefixes
	// sentry will identify the stack frame as "in_app"
	InAppPrefixes []string
}

func NewSentryOutput(DSN string, tags map[string]string) *Output {
	//client, _ := sentry.NewClient(DSN, tags)
	client, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:              DSN,
		Debug:            false,
		AttachStacktrace: true,
		SampleRate:       0,
		IgnoreErrors:     nil,
		BeforeSend:       nil,
		BeforeBreadcrumb: nil,
		Integrations:     nil,
		DebugWriter:      nil,
		Transport:        nil,
		ServerName:       "",
		Release:          "",
		Dist:             "",
		Environment:      "",
		MaxBreadcrumbs:   0,
		HTTPClient:       nil,
		HTTPTransport:    nil,
		HTTPProxy:        "",
		HTTPSProxy:       "",
		CaCerts:          nil,
	})
	return newOutput(client)
}

func NewSentryOutputWithClient(client *sentry.Client) *Output {
	return newOutput(client)
}

func newOutput(client *sentry.Client) *Output {
	hostname, _ := os.Hostname()
	return &Output{
		Timeout: 300 * time.Millisecond,
		StacktraceConfiguration: StackTraceConfiguration{
			Enable:        false,
			Level:         xlog.LevelError,
			Skip:          4,
			Context:       0,
			InAppPrefixes: nil,
		},
		client: client,
		host:   hostname,
	}
}

func getAndDel(fields map[string]interface{}, key string) (string, bool) {
	var (
		ok  bool
		v   interface{}
		val string
	)
	if v, ok = fields[key]; !ok {
		return "", false
	}

	if val, ok = v.(string); !ok {
		return "", false
	}
	delete(fields, key)
	return val, true
}

func getAndDelRequest(fields map[string]interface{}, key string) (*http.Request, bool) {
	var (
		ok  bool
		v   interface{}
		req *http.Request
	)
	if v, ok = fields[key]; !ok {
		return nil, false
	}
	if req, ok = v.(*http.Request); !ok || req == nil {
		return nil, false
	}
	delete(fields, key)
	return req, true
}

func cloneFields(src map[string]interface{}) map[string]interface{} {
	retval := make(map[string]interface{})
	for k, v := range src {
		retval[k] = v
	}
	return retval
}

// Write implements xlog.Output interface
func (o Output) Write(fields map[string]interface{}) error {
	fields = cloneFields(fields)
	level := xlogSeverityMap[fields[xlog.KeyLevel].(string)]

	if level < o.Level {
		return nil
	}

	packet := raven.NewPacket(fields[xlog.KeyMessage].(string))

	// In past, when xlog-sentry depends on raven-go (old SDK), raven.Packet struct was used here,
	// but sentry-go (new SDK) no longer provides Packet struct.
	//
	// According to the migration guide of Go SDK, sentry.Event struct should be used instead of raven.Packet.
	// https://docs.sentry.io/platforms/go/migration/#capturing-events
	event := sentry.NewEvent()
	event.Message = fields[xlog.KeyMessage].(string)
	event.Timestamp = fields[xlog.KeyTime].(time.Time)
	event.Level = severityMap[level]
	event.Logger = "xlog"

	delete(fields, xlog.KeyMessage)
	delete(fields, xlog.KeyTime)
	delete(fields, xlog.KeyLevel)
	delete(fields, xlog.KeyFile)

	if serverName, ok := getAndDel(fields, "host"); ok {
		event.ServerName = serverName
	} else if serverName, ok := getAndDel(fields, "server_name"); ok {
		event.ServerName = serverName
	} else {
		event.ServerName = o.host
	}
	if release, ok := getAndDel(fields, "release"); ok {
		event.Release = release
	}
	if culprit, ok := getAndDel(fields, "culprit"); ok {
		// In past, when xlog-sentry depends on raven-go, "culprit" data was assigned to `packet.Culprit`:
		//
		//   packet.Culprit = culprit
		//
		// Nowadays, "culprit" is deprecated, and sentry-go's sentry.Event struct doesn't expose `Culprit` field anymore.
		// https://forum.sentry.io/t/culprit-deprecated-in-favor-of-what/4871
		//
		// According to the following discussion comment, `Transaction` field should be used instead of `Culprit`.
		// https://forum.sentry.io/t/culprit-deprecated-in-favor-of-what/4871/6
		event.Transaction = culprit
	} else if role, ok := getAndDel(fields, "role"); ok {
		// ditto
		event.Transaction = role
	}
	if req, ok := getAndDelRequest(fields, "http_request"); ok {
		// In past, when xlog-sentry depends on raven-go, "http_request" data was assigned to `packet.Interfaces`:
		//
		//   packet.Interfaces = append(packet.Interfaces, raven.NewHttp(req))
		//
		// Nowadays, sentry-go's sentry.Event struct doesn't expose `Interfaces` field anymore, but exposes `Request` field.
		// Although `Extra` field can be used to assign request data, using `Request` field sounds more reasonable.
		event.Request = sentry.NewRequest(req)
	}

	fields["runtime.Version"] = runtime.Version()
	fields["runtime.NumCPU"] = runtime.NumCPU()
	fields["runtime.GOMAXPROCS"] = runtime.GOMAXPROCS(0)
	fields["runtime.NumGoroutine"] = runtime.NumGoroutine()

	stConfig := o.StacktraceConfiguration
	if stConfig.Enable && level <= stConfig.Level {
		//
		event.Extra["stacktrace"] = sentry.NewStacktrace()
		sentry.NewStacktrace()

		//event.Extra["stacktrace"] = currentStacktrace
		currentStacktrace := raven.NewStacktrace(stConfig.Skip, stConfig.Context, stConfig.InAppPrefixes)
		packet.Interfaces = append(packet.Interfaces, currentStacktrace)
	}

	event.Extra["fields"] = fields

	_ = o.client.CaptureEvent(event, nil, nil)
	//_, errCh := o.client.Capture(packet, nil)

	defer sentry.Flush(time.Second * 2)
	//timeout := o.Timeout
	//if timeout != 0 {
	//	timeoutCh := time.After(timeout)
	//	select {
	//	case err := <-errCh:
	//		return err
	//	case <-timeoutCh:
	//		return fmt.Errorf("No response from sentry server in %s", timeout)
	//	}
	//}

	return nil
}
