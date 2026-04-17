package dberrors

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetFatalState restores package-level fatal state between tests.
// Only used in tests.
func resetFatalState(t *testing.T) {
	t.Helper()
	fatalCount.Store(0)
	fatalHandlerMu.Lock()
	fatalHandler = nil
	fatalHandlerMu.Unlock()
	exitFunc = func(code int) {
		// Default for tests that don't set their own; panic to surface misuse.
		panic("exitFunc not configured in test")
	}
}

func TestHandleFatal_InvokesInstalledHandler(t *testing.T) {
	resetFatalState(t)

	var called int32
	var gotErr error
	var wg sync.WaitGroup
	wg.Add(1)

	SetFatalHandler(func(err error) {
		gotErr = err
		called++
		wg.Done()
	})

	// HandleFatal blocks forever after the handler returns, so run in a goroutine.
	go HandleFatal(errors.New("boom"))

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked within timeout")
	}

	require.Error(t, gotErr)
	assert.Equal(t, "boom", gotErr.Error())
	assert.Equal(t, int32(1), called)
}

func TestHandleFatal_NoHandlerCallsExit(t *testing.T) {
	resetFatalState(t)

	var exitCode int
	var exitCalled bool
	done := make(chan struct{})
	exitFunc = func(code int) {
		exitCode = code
		exitCalled = true
		close(done)
		// Simulate os.Exit by terminating the goroutine; HandleFatal must not continue.
		select {}
	}

	go HandleFatal(errors.New("no handler"))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exitFunc was not called within timeout")
	}

	assert.True(t, exitCalled)
	assert.Equal(t, 1, exitCode)
}

func TestHandleFatal_SecondCallBypassesHandlerAndExits(t *testing.T) {
	resetFatalState(t)

	handlerCalls := make(chan error, 4)
	SetFatalHandler(func(err error) {
		handlerCalls <- err
	})

	var exitCodes []int
	var mu sync.Mutex
	exitDone := make(chan struct{}, 1)
	exitFunc = func(code int) {
		mu.Lock()
		exitCodes = append(exitCodes, code)
		mu.Unlock()
		select {
		case exitDone <- struct{}{}:
		default:
		}
		// Simulate os.Exit: do not return to caller.
		select {}
	}

	// First call: handler invoked, goroutine then blocks forever in HandleFatal.
	go HandleFatal(errors.New("first"))

	select {
	case got := <-handlerCalls:
		assert.Equal(t, "first", got.Error())
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked for first fatal")
	}

	// Second call: must bypass handler and call exitFunc(1).
	go HandleFatal(errors.New("second"))

	select {
	case <-exitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("exitFunc was not called for second fatal")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, exitCodes, 1)
	assert.Equal(t, 1, exitCodes[0])

	// Handler must not have been called a second time.
	select {
	case extra := <-handlerCalls:
		t.Fatalf("handler invoked a second time with %v", extra)
	default:
	}
}
