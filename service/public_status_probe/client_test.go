package public_status_probe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type adapterFunc func(context.Context, Snapshot, Challenge) (string, error)

func (fn adapterFunc) Probe(ctx context.Context, snapshot Snapshot, challenge Challenge) (string, error) {
	return fn(ctx, snapshot, challenge)
}

type trackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type blockingBody struct {
	clock       *testClock
	readStarted chan struct{}
	release     chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
	reads       atomic.Int32
	closed      atomic.Bool
}

func newBlockingBody(clock *testClock) *blockingBody {
	return &blockingBody{
		clock:       clock,
		readStarted: make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (b *blockingBody) Read([]byte) (int, error) {
	b.reads.Add(1)
	b.readOnce.Do(func() { close(b.readStarted) })
	<-b.release
	return 0, io.EOF
}

func (b *blockingBody) Close() error {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		b.clock.Advance(10 * time.Second)
		close(b.release)
	})
	return nil
}

type testClock struct {
	current time.Time
}

func (c *testClock) Now() time.Time {
	return c.current
}

func (c *testClock) Advance(elapsed time.Duration) {
	c.current = c.current.Add(elapsed)
}

type timedSegmentReader struct {
	clock    *testClock
	segments []string
	delays   []time.Duration
	current  *strings.Reader
	next     int
}

func (r *timedSegmentReader) Read(buffer []byte) (int, error) {
	if r.current == nil {
		if r.next == len(r.segments) {
			return 0, io.EOF
		}
		r.clock.Advance(r.delays[r.next])
		r.current = strings.NewReader(r.segments[r.next])
		r.next++
	}

	read, err := r.current.Read(buffer)
	if r.current.Len() == 0 {
		r.current = nil
	}
	if read > 0 && err == io.EOF {
		err = nil
	}
	return read, err
}

func TestPingAnyHEADResponseIsReachableWithoutGETFallback(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("arbitrary head body")}
	var methods []string
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		assert.Equal(t, "/", req.URL.Path)
		assert.Empty(t, req.URL.RawQuery)
		return &http.Response{
			StatusCode:    http.StatusServiceUnavailable,
			Body:          body,
			ContentLength: int64(len("arbitrary head body")),
			Request:       req,
		}, nil
	})}

	times := []time.Time{time.Unix(0, 0), time.UnixMilli(137)}
	result := pingWithClock(context.Background(), client, "https://example.com/provider/path?secret=value", time.Second, func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	})

	require.True(t, result.Reachable)
	require.NotNil(t, result.LatencyMS)
	assert.EqualValues(t, 137, *result.LatencyMS)
	assert.Empty(t, result.Code)
	assert.Equal(t, []string{http.MethodHead}, methods)
	assert.True(t, body.closed.Load())
}

func TestPingRetriesGETOnlyAfterHEADTransportFailureAndClosesBody(t *testing.T) {
	getBody := &trackingBody{Reader: strings.NewReader("reachable")}
	var methods []string
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodHead {
			return nil, errors.New("head transport failed")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: getBody, Request: req}, nil
	})}

	result := Ping(context.Background(), client, "https://example.com/v1?x=1", time.Second)

	require.True(t, result.Reachable)
	assert.Equal(t, []string{http.MethodHead, http.MethodGet}, methods)
	assert.True(t, getBody.closed.Load())
}

func TestPingRecordsHEADOrGETResponseBeforeClosingBody(t *testing.T) {
	for _, responseMethod := range []string{http.MethodHead, http.MethodGet} {
		t.Run(responseMethod, func(t *testing.T) {
			const receiptLatency = 137 * time.Millisecond
			clock := &testClock{current: time.Unix(0, 0)}
			body := newBlockingBody(clock)
			var methods []string
			client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				methods = append(methods, req.Method)
				if req.Method != responseMethod {
					return nil, errors.New("head transport failed")
				}
				clock.Advance(receiptLatency)
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body, Request: req}, nil
			})}

			done := make(chan PingResult, 1)
			go func() {
				done <- pingWithClock(context.Background(), client, "https://example.com/provider", time.Second, clock.Now)
			}()

			var result PingResult
			select {
			case result = <-done:
			case <-body.readStarted:
				_ = body.Close()
				<-done
				t.Fatal("Ping read the response body")
			case <-time.After(time.Second):
				_ = body.Close()
				<-done
				t.Fatal("Ping blocked after receiving response headers")
			}

			require.True(t, result.Reachable)
			require.NotNil(t, result.LatencyMS)
			assert.EqualValues(t, receiptLatency.Milliseconds(), *result.LatencyMS)
			assert.Zero(t, body.reads.Load())
			assert.True(t, body.closed.Load())
			if responseMethod == http.MethodHead {
				assert.Equal(t, []string{http.MethodHead}, methods)
			} else {
				assert.Equal(t, []string{http.MethodHead, http.MethodGet}, methods)
			}
		})
	}
}

func TestPingSharesOneTimeoutAcrossHEADAndGET(t *testing.T) {
	var deadlines []time.Time
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		deadlines = append(deadlines, deadline)
		return nil, errors.New("transport failed")
	})}

	result := Ping(context.Background(), client, "https://example.com", time.Minute)

	assert.False(t, result.Reachable)
	assert.Equal(t, ErrorNetwork, result.Code)
	require.Len(t, deadlines, 2)
	assert.Equal(t, deadlines[0], deadlines[1])
}

func TestPingDisablesRedirects(t *testing.T) {
	var redirected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirected" {
			redirected.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, req, "/redirected", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	result := Ping(context.Background(), server.Client(), server.URL, time.Second)

	assert.True(t, result.Reachable)
	assert.False(t, redirected.Load())
}

func TestNilClientCopiesShareDedicatedDefaultTransport(t *testing.T) {
	shared := defaultHTTPClient()
	assert.Same(t, shared, defaultHTTPClient())
	assert.NotSame(t, http.DefaultClient, shared)
	assert.NotSame(t, http.DefaultTransport, shared.Transport)

	first := clientWithoutRedirects(nil)
	second := clientWithoutRedirects(nil)
	assert.NotSame(t, first, second)
	assert.Same(t, shared.Transport, first.Transport)
	assert.Same(t, first.Transport, second.Transport)
	require.NotNil(t, first.CheckRedirect)
	assert.ErrorIs(t, first.CheckRedirect(nil, nil), http.ErrUseLastResponse)
}

func TestPingRejectsInvalidTargets(t *testing.T) {
	for _, target := range []string{
		"",
		"example.com/path",
		"ftp://example.com/path",
		"https:///missing-host",
		"https://user:password@example.com/path",
	} {
		t.Run(target, func(t *testing.T) {
			result := Ping(context.Background(), nil, target, time.Second)
			assert.False(t, result.Reachable)
			assert.Nil(t, result.LatencyMS)
			assert.Equal(t, ErrorInvalidTarget, result.Code)
		})
	}
}

func TestOriginURLAcceptsValidPorts(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "domain without port", target: "https://example.com/provider", want: "https://example.com/"},
		{name: "domain minimum port", target: "http://example.com:1/provider", want: "http://example.com:1/"},
		{name: "IPv4 maximum port", target: "https://192.0.2.1:65535/provider", want: "https://192.0.2.1:65535/"},
		{name: "bracketed IPv6 without port", target: "http://[2001:db8::1]/provider", want: "http://[2001:db8::1]/"},
		{name: "bracketed IPv6 with port", target: "https://[2001:db8::1]:443/provider", want: "https://[2001:db8::1]:443/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := originURL(test.target)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestOriginURLRejectsInvalidPorts(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "zero", target: "https://example.com:0/provider"},
		{name: "negative", target: "https://[2001:db8::1]:-1/provider"},
		{name: "non-numeric", target: "https://example.com:not-a-port/provider"},
		{name: "above maximum", target: "https://192.0.2.1:65536/provider"},
		{name: "integer overflow", target: "https://example.com:999999999999999999999/provider"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := originURL(test.target)
			require.Error(t, err)
			assert.Equal(t, ErrorInvalidTarget, ErrorCodeOf(err))
		})
	}
}

func TestPingMapsTimeoutAndCancellation(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}

	t.Run("timeout", func(t *testing.T) {
		result := Ping(context.Background(), client, "https://example.com", time.Millisecond)
		assert.False(t, result.Reachable)
		assert.Equal(t, ErrorTimeout, result.Code)
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := Ping(ctx, client, "https://example.com", time.Second)
		assert.False(t, result.Reachable)
		assert.Equal(t, ErrorTimeout, result.Code)
	})
}

func TestReadBoundedResponseCapsAndClosesBody(t *testing.T) {
	body := &trackingBody{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), MaxResponseBytes+1))}
	_, err := ReadBoundedResponse(&http.Response{StatusCode: http.StatusOK, Body: body})

	require.Error(t, err)
	assert.Equal(t, ErrorResponseTooLarge, ErrorCodeOf(err))
	assert.True(t, body.closed.Load())
}

func TestReadBoundedResponseRejectsProviderWithoutLeakingBody(t *testing.T) {
	const providerBody = "credential=super-secret-provider-message"
	body := &trackingBody{Reader: strings.NewReader(providerBody)}
	_, err := ReadBoundedResponse(&http.Response{StatusCode: http.StatusUnauthorized, Body: body})

	require.Error(t, err)
	assert.Equal(t, ErrorProviderRejected, ErrorCodeOf(err))
	assert.NotContains(t, err.Error(), providerBody)
	assert.True(t, body.closed.Load())
}

func TestDoBoundedMapsTransportErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial contained credential")
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	_, err = DoBounded(client, req)
	require.Error(t, err)
	assert.Equal(t, ErrorNetwork, ErrorCodeOf(err))
	assert.Equal(t, string(ErrorNetwork), err.Error())
}

func TestErrorCodeSanitizersAllowOnlyStableCodes(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want ErrorCode
	}{
		{name: "unsupported provider", code: ErrorUnsupportedProvider, want: ErrorUnsupportedProvider},
		{name: "timeout", code: ErrorTimeout, want: ErrorTimeout},
		{name: "network", code: ErrorNetwork, want: ErrorNetwork},
		{name: "provider rejected", code: ErrorProviderRejected, want: ErrorProviderRejected},
		{name: "empty response", code: ErrorEmptyResponse, want: ErrorEmptyResponse},
		{name: "validation failed", code: ErrorValidationFailed, want: ErrorValidationFailed},
		{name: "invalid target", code: ErrorInvalidTarget, want: ErrorInvalidTarget},
		{name: "documented response too large", code: ErrorResponseTooLarge, want: ErrorResponseTooLarge},
		{name: "unknown malicious code", code: ErrorCode("provider_error: api_key=secret"), want: ErrorNetwork},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &ProbeError{Code: test.code}
			assert.Equal(t, string(test.want), err.Error())
			assert.Equal(t, test.want, ErrorCodeOf(err))
			assert.Equal(t, test.want, transportOrProbeErrorCode(context.Background(), err))
		})
	}

	var nilProbeErr *ProbeError
	assert.Empty(t, nilProbeErr.Error())
}

func TestChallengeNonceIsRandomAndExplicit(t *testing.T) {
	first, err := NewChallenge()
	require.NoError(t, err)
	second, err := NewChallenge()
	require.NoError(t, err)

	assert.Regexp(t, regexp.MustCompile(`^psp_[a-f0-9]{24}$`), first.Expected)
	assert.NotEqual(t, first.Expected, second.Expected)
	assert.Contains(t, first.Prompt, first.Expected)
	assert.NotContains(t, first.Expected, "secret")
}

func TestContainsExpectedTokenAllowsExplicitWrappedResponses(t *testing.T) {
	const token = "psp_0123456789abcdef01234567"
	const wrongToken = "psp_aaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "exact", value: token, want: true},
		{name: "normalized case and external whitespace", value: " \nPSP_0123456789ABCDEF01234567\t", want: true},
		{name: "answer label", value: "answer: " + token, want: true},
		{name: "chinese answer label and punctuation", value: "答案是：" + token + "。", want: true},
		{name: "markdown wrapper", value: "`" + token + "`", want: true},
		{name: "punctuation suffix", value: token + ".", want: true},
		{name: "combined label and markdown", value: "answer: `" + token + "`", want: false},
		{name: "prompt echo", value: "Reply with exactly this token and no other text: " + token, want: false},
		{name: "short prompt echo", value: "reply with token: " + token, want: false},
		{name: "explanation", value: "The answer is " + token, want: false},
		{name: "repeated token", value: token + " " + token, want: false},
		{name: "wrong token before expected", value: wrongToken + "; " + token, want: false},
		{name: "prefixed", value: "prefix" + token, want: false},
		{name: "suffixed", value: token + "suffix", want: false},
		{name: "invalid expected shape", value: token, want: false},
		{name: "empty expected", value: token, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := token
			if test.name == "empty expected" {
				expected = ""
			} else if test.name == "invalid expected shape" {
				expected = token + "."
			}
			assert.Equal(t, test.want, containsExpectedToken(test.value, expected))
		})
	}
}

func TestRunConversationValidationAndLatencyStates(t *testing.T) {
	fixedChallenge := func() (Challenge, error) {
		return Challenge{Expected: "psp_0123456789abcdef01234567", Prompt: "reply with token"}, nil
	}
	clock := func(elapsed time.Duration) func() time.Time {
		values := []time.Time{time.Unix(0, 0), time.Unix(0, 0).Add(elapsed)}
		return func() time.Time {
			value := values[0]
			values = values[1:]
			return value
		}
	}

	tests := []struct {
		name    string
		output  string
		elapsed time.Duration
		state   ProbeState
		code    ErrorCode
		latency int64
	}{
		{name: "success normalized case", output: "  PSP_0123456789ABCDEF01234567  ", elapsed: 1200 * time.Millisecond, state: StateOperational, latency: 1200},
		{name: "degraded", output: "psp_0123456789abcdef01234567", elapsed: 6001 * time.Millisecond, state: StateDegraded, latency: 6001},
		{name: "prompt echo", output: "reply with token psp_0123456789abcdef01234567", elapsed: 500 * time.Millisecond, state: StateValidationFailed, code: ErrorValidationFailed, latency: 500},
		{name: "answer label", output: "answer: psp_0123456789abcdef01234567", elapsed: 500 * time.Millisecond, state: StateOperational, latency: 500},
		{name: "nonce mismatch", output: "psp_aaaaaaaaaaaaaaaaaaaaaaaa", elapsed: 400 * time.Millisecond, state: StateValidationFailed, code: ErrorValidationFailed, latency: 400},
		{name: "empty", output: " \n\t ", elapsed: 300 * time.Millisecond, state: StateFailed, code: ErrorEmptyResponse, latency: 300},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := adapterFunc(func(context.Context, Snapshot, Challenge) (string, error) {
				return test.output, nil
			})
			result := runConversation(context.Background(), adapter, Snapshot{}, time.Second, DefaultDegradedThreshold, clock(test.elapsed), fixedChallenge)
			assert.Equal(t, test.state, result.State)
			assert.Equal(t, test.code, result.Code)
			require.NotNil(t, result.LatencyMS)
			assert.Equal(t, test.latency, *result.LatencyMS)
		})
	}
}

func TestRunConversationMapsTimeout(t *testing.T) {
	adapter := adapterFunc(func(ctx context.Context, _ Snapshot, _ Challenge) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	result := RunConversation(context.Background(), adapter, Snapshot{}, time.Millisecond, time.Second)

	assert.Equal(t, StateFailed, result.State)
	assert.Equal(t, ErrorTimeout, result.Code)
	assert.Nil(t, result.LatencyMS)
}

func TestRunConversationMeasuresThroughCompleteBoundedBodyRead(t *testing.T) {
	const token = "psp_0123456789abcdef01234567"
	clock := &testClock{current: time.Unix(0, 0)}
	reader := &timedSegmentReader{
		clock:    clock,
		segments: []string{"psp_", "0123456789ab", "cdef01234567"},
		delays:   []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
	}
	body := &trackingBody{Reader: reader}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Request: req}, nil
	})}
	adapter := adapterFunc(func(ctx context.Context, _ Snapshot, _ Challenge) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/v1/probe", nil)
		if err != nil {
			return "", err
		}
		response, err := DoBounded(client, req)
		return string(response), err
	})
	challengeFactory := func() (Challenge, error) {
		return Challenge{Expected: token, Prompt: "reply with token"}, nil
	}

	result := runConversation(context.Background(), adapter, Snapshot{}, time.Minute, 6*time.Second, clock.Now, challengeFactory)

	assert.Equal(t, StateDegraded, result.State)
	assert.Empty(t, result.Code)
	require.NotNil(t, result.LatencyMS)
	assert.EqualValues(t, 7000, *result.LatencyMS)
	assert.Equal(t, len(reader.segments), reader.next)
	assert.True(t, body.closed.Load())
}

func TestPingFailureDoesNotChangeSuccessfulConversationState(t *testing.T) {
	ping := PingResult{Reachable: false, Code: ErrorNetwork}
	latency := int64(750)
	conversation := ConversationResult{State: StateOperational, LatencyMS: &latency}

	result := CombineResults(ping, conversation)

	assert.Equal(t, StateOperational, result.State)
	assert.Equal(t, ErrorNetwork, result.PingErrorCode)
	assert.Empty(t, result.ErrorCode)
}
