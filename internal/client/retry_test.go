package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func clienteDe(t *testing.T, srv *httptest.Server, o Opcoes) *Client {
	t.Helper()
	return New(srv.URL, "DEFAULT", "DEFAULT", "tok", o)
}

// GET repete em 503 e devolve o corpo da tentativa que deu certo.
func TestRetryGETEm503(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"id":"ok"}`))
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 4})
	var out struct{ ID string }
	if err := c.do(context.Background(), http.MethodGet, "/x", nil, &out); err != nil {
		t.Fatalf("deveria ter passado na terceira: %v", err)
	}
	if out.ID != "ok" || atomic.LoadInt32(&n) != 3 {
		t.Errorf("quis id=ok em 3 chamadas, veio id=%q em %d", out.ID, n)
	}
}

// POST nao repete em 502: repetir arriscaria criar o objeto duas vezes.
func TestPOSTNaoRepeteEm502(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 4})
	err := c.do(context.Background(), http.MethodPost, "/x", map[string]string{"a": "b"}, nil)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Errorf("POST em 502 deveria ser uma unica chamada, foram %d", n)
	}
}

// Mas POST repete em 429, porque ai o servidor diz que nao processou.
func TestPOSTRepeteEm429(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 4})
	if err := c.do(context.Background(), http.MethodPost, "/x", map[string]string{"a": "b"}, nil); err != nil {
		t.Fatalf("deveria ter passado: %v", err)
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Errorf("quis 2 chamadas, foram %d", n)
	}
}

// 404 nao repete, e continua reconhecivel por NotFound.
func TestNaoRepete404(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 4})
	err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
	if !NotFound(err) {
		t.Fatalf("esperava NotFound, veio %v", err)
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Errorf("404 deveria ser uma unica chamada, foram %d", n)
	}
}

// O limite de simultaneidade e respeitado.
func TestLimiteDeSimultaneidade(t *testing.T) {
	var emVoo, pico int32
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := atomic.AddInt32(&emVoo, 1)
		mu.Lock()
		if v > pico {
			pico = v
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&emVoo, -1)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 1, Simultaneas: 2})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.do(context.Background(), http.MethodGet, "/x", nil, nil)
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if pico > 2 {
		t.Errorf("pico de %d chamadas simultaneas, limite era 2", pico)
	}
}

// Esgotar as tentativas devolve erro que cita o ultimo status.
func TestEsgotaTentativas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := clienteDe(t, srv, Opcoes{Tentativas: 2})
	err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("esperava erro")
	}
	var e *Error
	if !asErr(err, &e) || e.Status != http.StatusServiceUnavailable {
		t.Errorf("esperava o 503 encapsulado, veio %v", err)
	}
}

// ctx cancelado interrompe a espera entre tentativas.
func TestContextCancelaEspera(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c := clienteDe(t, srv, Opcoes{Tentativas: 8})
	inicio := time.Now()
	if err := c.do(ctx, http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("esperava erro")
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("deveria ter desistido rapido com ctx cancelado, levou %s", d)
	}
}
