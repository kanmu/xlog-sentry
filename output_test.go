package xlogsentry

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/rs/xlog"
)

type dummyTransportArgs struct {
	url        string
	authHeader string
	packet     *raven.Packet
}

type dummyTransport struct {
	roundtripChan chan<- dummyTransportArgs
	t             *time.Timer
}

func (dtx *dummyTransport) Send(url, authHeader string, packet *raven.Packet) error {
	dtx.t.Stop()
	dtx.roundtripChan <- dummyTransportArgs{url, authHeader, packet}
	return nil
}

func dummyClient(roundtripChan chan<- dummyTransportArgs) *raven.Client {
	client, err := raven.New("http://secret@localhost/id")
	if err != nil {
		panic(err)
	}
	client.Transport = &dummyTransport{roundtripChan, time.AfterFunc(time.Second, func() {
		close(roundtripChan)
	})}
	return client
}

func typesOf(is []raven.Interface) []reflect.Type {
	retval := make([]reflect.Type, 0, len(is))
	for _, i := range is {
		retval = append(retval, reflect.TypeOf(i))
	}
	return retval
}

func diff(a []reflect.Type, b []reflect.Type) bool {
outer1:
	for _, i := range a {
		for _, j := range b {
			if i == j {
				continue outer1
			}
		}
		return true
	}

outer2:
	for _, i := range b {
		for _, j := range a {
			if i == j {
				continue outer2
			}
		}
		return true
	}

	return false
}

func TestWriteBasic(t *testing.T) {
	tests := []struct {
		stEna bool
		stLv  xlog.Level
		tlv   xlog.Level
		in    map[string]interface{}
		sent  bool
		ts    time.Time
		lv    raven.Severity
		msg   string
		ifs   []reflect.Type
	}{
		{
			stEna: true,
			stLv:  xlog.LevelInfo,
			tlv:   xlog.LevelInfo,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: true,
			ts:   time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			lv:   raven.INFO,
			msg:  "message",
			ifs:  []reflect.Type{reflect.TypeOf(&raven.Stacktrace{})},
		},
		{
			stEna: false,
			stLv:  xlog.LevelInfo,
			tlv:   xlog.LevelInfo,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: true,
			ts:   time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			lv:   raven.INFO,
			msg:  "message",
			ifs:  []reflect.Type{},
		},
		{
			stEna: true,
			stLv:  xlog.LevelError,
			tlv:   xlog.LevelInfo,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: true,
			ts:   time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			lv:   raven.INFO,
			msg:  "message",
			ifs:  []reflect.Type{reflect.TypeOf(&raven.Stacktrace{})},
		},
		{
			stEna: true,
			stLv:  xlog.LevelDebug,
			tlv:   xlog.LevelInfo,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: true,
			ts:   time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			lv:   raven.INFO,
			msg:  "message",
			ifs:  []reflect.Type{},
		},
		{
			stEna: true,
			stLv:  xlog.LevelError,
			tlv:   xlog.LevelError,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: false,
		},
		{
			stEna: true,
			stLv:  xlog.LevelInfo,
			tlv:   xlog.LevelError,
			in: map[string]interface{}{
				xlog.KeyMessage: "message",
				xlog.KeyTime:    time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				xlog.KeyLevel:   "info",
				xlog.KeyFile:    "file",
			},
			sent: false,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test #%d", i), func(t *testing.T) {
			roundtripChan := make(chan dummyTransportArgs, 1)
			target := Output{
				Timeout: 1 * time.Second,
				Level:   test.tlv,
				StacktraceConfiguration: StackTraceConfiguration{
					test.stEna,
					test.stLv,
					0,
					5,
					[]string{},
				},
				client: dummyClient(roundtripChan),
				host:   "host",
			}

			target.Write(test.in)
			args, ok := <-roundtripChan
			if !test.sent {
				if ok {
					t.Log("data has been sent where it shouldn't be")
					t.Fail()
				}
				return
			}

			if !ok {
				t.Log("no data has been sent")
				t.Fail()
				return
			}

			if "http://localhost/api/id/store/" != args.url {
				t.Logf(`%+v != args.url (got %+v)`, "http://localhost/api/id/store/", args.url)
				t.Fail()
			}
			if strings.Index(args.authHeader, "secret") < 0 {
				t.Logf(`%+v not in args.authHeader (got %+v)`, "secret", args.authHeader)
				t.Fail()
			}
			if test.ts != time.Time(args.packet.Timestamp) {
				t.Logf(`%+v != args.packet.Timestamp (got %+v)`, test.ts.Format(time.RFC3339), args.packet.Timestamp.Format(time.RFC3339))
				t.Fail()
			}
			if test.lv != args.packet.Level {
				t.Logf(`%+v != args.packet.Level (got %+v)`, test.lv, args.packet.Level)
				t.Fail()
			}
			if test.msg != args.packet.Message {
				t.Logf(`%+v != args.Messsage (got %+v)`, test.msg, args.packet.Message)
				t.Fail()
			}
			typs := typesOf(args.packet.Interfaces)
			if diff(test.ifs, typs) {
				t.Logf(`%+v != args.packet.Interfaces (got %+v)`, test.ifs, typs)
				t.Fail()
			}
			for _, k := range []string{xlog.KeyMessage, xlog.KeyTime, xlog.KeyLevel, xlog.KeyFile} {
				if _, ok := args.packet.Extra[k]; ok {
					t.Logf(`key %+v exists in args.packet.Extra`, k)
					t.Fail()
				}
				if _, ok := test.in[k]; !ok {
					t.Logf(`key %+v does not exist in fields`, k)
					t.Fail()
				}
			}
		})
	}
}
