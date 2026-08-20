package public_status_probe

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var defaultHTTPClient = sync.OnceValue(NewHTTPClient)

func NewHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   8 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: DefaultConversationTimeout,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: noRedirect,
	}
}

func noRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func Ping(ctx context.Context, client *http.Client, baseURL string, timeout time.Duration) PingResult {
	return pingWithClock(ctx, client, baseURL, timeout, time.Now)
}

func pingWithClock(ctx context.Context, client *http.Client, baseURL string, timeout time.Duration, now func() time.Time) PingResult {
	origin, err := originURL(baseURL)
	if err != nil {
		return PingResult{Code: ErrorInvalidTarget}
	}
	if timeout <= 0 {
		timeout = DefaultPingTimeout
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := now()

	probeClient := clientWithoutRedirects(client)
	resp, err := sendPingRequest(requestCtx, probeClient, http.MethodHead, origin)
	if err == nil {
		latency := elapsedMilliseconds(start, now())
		result := PingResult{Reachable: true, LatencyMS: &latency}
		closeResponse(resp)
		return result
	}

	resp, err = sendPingRequest(requestCtx, probeClient, http.MethodGet, origin)
	if err == nil {
		latency := elapsedMilliseconds(start, now())
		result := PingResult{Reachable: true, LatencyMS: &latency}
		closeResponse(resp)
		return result
	}
	return PingResult{Code: transportErrorCode(requestCtx, err)}
}

func originURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed == nil {
		return "", codedError(ErrorInvalidTarget)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", codedError(ErrorInvalidTarget)
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", codedError(ErrorInvalidTarget)
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			return "", codedError(ErrorInvalidTarget)
		}
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String(), nil
}

func clientWithoutRedirects(client *http.Client) *http.Client {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &http.Client{
		Transport:     client.Transport,
		CheckRedirect: noRedirect,
		Jar:           client.Jar,
		Timeout:       client.Timeout,
	}
}

func sendPingRequest(ctx context.Context, client *http.Client, method, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_ = resp.Body.Close()
}

// DoBounded disables redirects, uses a shared dedicated client when client is
// nil, and always closes any response body before returning.
func DoBounded(client *http.Client, req *http.Request) ([]byte, error) {
	if req == nil {
		return nil, codedError(ErrorInvalidTarget)
	}
	client = clientWithoutRedirects(client)
	resp, err := client.Do(req)
	if err != nil {
		return nil, codedError(transportErrorCode(req.Context(), err))
	}
	return ReadBoundedResponse(resp)
}

// ReadBoundedResponse takes ownership of resp.Body and always closes it.
func ReadBoundedResponse(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, codedError(ErrorEmptyResponse)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes+1))
		return nil, codedError(ErrorProviderRejected)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, codedError(responseReadErrorCode(resp, err))
	}
	if len(body) > MaxResponseBytes {
		return nil, codedError(ErrorResponseTooLarge)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, codedError(ErrorEmptyResponse)
	}
	return body, nil
}

func responseReadErrorCode(resp *http.Response, err error) ErrorCode {
	if resp != nil && resp.Request != nil {
		return transportErrorCode(resp.Request.Context(), err)
	}
	return transportErrorCode(context.Background(), err)
}

func transportErrorCode(ctx context.Context, err error) ErrorCode {
	if ctx != nil && ctx.Err() != nil {
		return ErrorTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrorTimeout
	}
	return ErrorNetwork
}

func NewChallenge() (Challenge, error) {
	return newChallenge(rand.Reader)
}

var semanticChallengeCategories = []struct {
	name  string
	words []string
}{
	{name: "animal", words: []string{"cat", "dog", "tiger", "horse", "rabbit", "eagle", "dolphin", "wolf"}},
	{name: "fruit", words: []string{"apple", "banana", "grape", "mango", "peach", "lemon", "cherry", "pear"}},
	{name: "color", words: []string{"red", "blue", "green", "yellow", "purple", "pink", "black", "white"}},
	{name: "country", words: []string{"japan", "france", "brazil", "canada", "egypt", "india", "norway", "kenya"}},
	{name: "metal", words: []string{"iron", "gold", "copper", "silver", "zinc", "nickel", "lead", "tin"}},
	{name: "vehicle", words: []string{"car", "truck", "train", "bicycle", "airplane", "boat", "scooter", "tram"}},
	{name: "instrument", words: []string{"piano", "guitar", "violin", "drum", "flute", "trumpet", "harp", "cello"}},
	{name: "drink", words: []string{"coffee", "tea", "juice", "milk", "soda", "water", "cocoa", "lemonade"}},
}

func newChallenge(random io.Reader) (Challenge, error) {
	bytes := make([]byte, 12)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return Challenge{}, codedError(ErrorNetwork)
	}

	categoryIndex := int(bytes[0]) % len(semanticChallengeCategories)
	category := semanticChallengeCategories[categoryIndex]
	expected := category.words[int(bytes[1])%len(category.words)]
	options := []string{expected}
	for index := 0; len(options) < 5; index++ {
		otherCategory := semanticChallengeCategories[(categoryIndex+1+index)%len(semanticChallengeCategories)]
		candidate := otherCategory.words[int(bytes[6+index])%len(otherCategory.words)]
		options = append(options, candidate)
	}
	for index := len(options) - 1; index > 0; index-- {
		swapIndex := int(bytes[index+2]) % (index + 1)
		options[index], options[swapIndex] = options[swapIndex], options[index]
	}

	return Challenge{
		Expected: expected,
		Prompt: fmt.Sprintf(
			"Pick the word that belongs to the %s category. Reply with ONLY that one word.\nCategory: %s\nOptions: %s\nA:",
			category.name,
			category.name,
			strings.Join(options, ", "),
		),
	}, nil
}

func RunConversation(ctx context.Context, adapter Adapter, snapshot Snapshot, timeout, degradedThreshold time.Duration) ConversationResult {
	return runConversation(ctx, adapter, snapshot, timeout, degradedThreshold, time.Now, NewChallenge)
}

func runConversation(
	ctx context.Context,
	adapter Adapter,
	snapshot Snapshot,
	timeout time.Duration,
	degradedThreshold time.Duration,
	now func() time.Time,
	challengeFactory func() (Challenge, error),
) ConversationResult {
	if adapter == nil {
		return ConversationResult{State: StateFailed, Code: ErrorUnsupportedProvider}
	}
	if timeout <= 0 {
		timeout = DefaultConversationTimeout
	}
	if degradedThreshold <= 0 {
		degradedThreshold = DefaultDegradedThreshold
	}

	challenge, err := challengeFactory()
	if err != nil {
		return ConversationResult{State: StateFailed, Code: ErrorCodeOf(err)}
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := now()
	output, err := adapter.Probe(requestCtx, snapshot, challenge)
	elapsed := now().Sub(start)
	if elapsed < 0 {
		elapsed = 0
	}
	if err != nil {
		code := transportOrProbeErrorCode(requestCtx, err)
		state := StateFailed
		if code == ErrorValidationFailed {
			state = StateValidationFailed
		}
		return ConversationResult{State: state, Code: code}
	}

	latency := elapsed.Milliseconds()
	if len(output) > MaxResponseBytes {
		return ConversationResult{State: StateFailed, Code: ErrorResponseTooLarge}
	}
	normalized := normalizedOutput(output)
	if normalized == "" {
		return ConversationResult{State: StateFailed, LatencyMS: &latency, Code: ErrorEmptyResponse}
	}
	if !containsExpectedToken(normalized, challenge.Expected) {
		return ConversationResult{State: StateValidationFailed, LatencyMS: &latency, Code: ErrorValidationFailed}
	}
	if elapsed > degradedThreshold {
		return ConversationResult{State: StateDegraded, LatencyMS: &latency}
	}
	return ConversationResult{State: StateOperational, LatencyMS: &latency}
}

func transportOrProbeErrorCode(ctx context.Context, err error) ErrorCode {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	var probeErr *ProbeError
	if errors.As(err, &probeErr) && probeErr != nil {
		return stableErrorCode(probeErr.Code)
	}
	return transportErrorCode(ctx, err)
}

func containsExpectedToken(value, expected string) bool {
	expectedTokens := validationTokens(expected)
	if len(expectedTokens) != 1 {
		return false
	}

	valueTokens := validationTokens(value)
	if len(valueTokens) == 0 || len(valueTokens) > maxValidationTokens {
		return false
	}

	answerCount := 0
	for _, token := range valueTokens {
		if token == expectedTokens[0] {
			answerCount++
		}
	}
	return answerCount == 1
}

func validationTokens(value string) []string {
	normalized := strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)
	return strings.Fields(normalized)
}

func elapsedMilliseconds(start, end time.Time) int64 {
	elapsed := end.Sub(start).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
